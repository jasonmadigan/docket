package gh

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

var now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)

type roundTrip func(*http.Request) (*http.Response, error)

func (f roundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func respond(status int, header http.Header, body string) roundTrip {
	return func(r *http.Request) (*http.Response, error) {
		h := http.Header{"Content-Type": {"application/json"}}
		for k, v := range header {
			h[k] = v
		}
		return &http.Response{
			StatusCode: status,
			Header:     h,
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    r,
		}, nil
	}
}

func client(t *testing.T, rt http.RoundTripper) *Client {
	t.Helper()
	c, err := newClient(api.ClientOptions{Host: "github.com", AuthToken: "token", Transport: rt})
	if err != nil {
		t.Fatal(err)
	}
	c.now = func() time.Time { return now }
	return c
}

type viewer struct {
	Viewer struct {
		Login string `json:"login"`
	} `json:"viewer"`
}

func TestDoDecodesData(t *testing.T) {
	var out viewer
	err := client(t, respond(200, nil, `{"data":{"viewer":{"login":"me"}}}`)).Do(context.Background(), "q", nil, &out)
	if err != nil {
		t.Fatal(err)
	}
	if out.Viewer.Login != "me" {
		t.Fatalf("login = %q", out.Viewer.Login)
	}
}

func TestDoKeepsPartialData(t *testing.T) {
	body := `{"data":{"viewer":{"login":"me"}},"errors":[` +
		`{"type":"FORBIDDEN","message":"SAML enforced"},{"type":"FORBIDDEN","message":"SAML enforced"}]}`
	var out viewer
	err := client(t, respond(200, nil, body)).Do(context.Background(), "q", nil, &out)
	var partial *PartialError
	if !errors.As(err, &partial) {
		t.Fatalf("err = %v, want *PartialError", err)
	}
	if !reflect.DeepEqual(partial.Messages, []string{"SAML enforced"}) {
		t.Fatalf("messages = %q", partial.Messages)
	}
	if out.Viewer.Login != "me" {
		t.Fatalf("partial data lost: %+v", out)
	}
}

func limitedUntil(until time.Time) func(*testing.T, error) {
	return func(t *testing.T, err error) {
		t.Helper()
		var limited *RateLimitedError
		if !errors.As(err, &limited) {
			t.Fatalf("err = %v, want *RateLimitedError", err)
		}
		if !limited.Until.Equal(until) {
			t.Fatalf("until = %v, want %v", limited.Until, until)
		}
	}
}

func TestDoMapsErrors(t *testing.T) {
	cases := []struct {
		name  string
		rt    http.RoundTripper
		check func(*testing.T, error)
	}{
		{"bad token", respond(401, nil, `{"message":"Bad credentials"}`), func(t *testing.T, err error) {
			if !errors.Is(err, ErrUnauthorised) {
				t.Fatalf("err = %v, want ErrUnauthorised", err)
			}
		}},
		{"retry after", respond(403, http.Header{"Retry-After": {"30"}}, `{"message":"secondary rate limit"}`),
			limitedUntil(now.Add(30 * time.Second))},
		{"primary limit", respond(403, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1790000000"}}, `{"message":"rate limit"}`),
			limitedUntil(time.Unix(1790000000, 0))},
		{"graphql limit", respond(200, nil, `{"data":null,"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`),
			limitedUntil(time.Time{})},
		{"server error", respond(502, nil, `{"message":"bad gateway"}`), func(t *testing.T, err error) {
			var httpErr *api.HTTPError
			if !errors.As(err, &httpErr) || httpErr.StatusCode != 502 {
				t.Fatalf("err = %v, want a 502 *api.HTTPError", err)
			}
		}},
		{"network", roundTrip(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") }),
			func(t *testing.T, err error) {
				if err == nil || !strings.Contains(err.Error(), "offline") {
					t.Fatalf("err = %v, want the transport error", err)
				}
			}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var out viewer
			c.check(t, client(t, c.rt).Do(context.Background(), "q", nil, &out))
		})
	}
}
