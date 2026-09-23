package gh

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/cli/go-gh/v2/pkg/auth"
)

const host = "github.com"

var ErrUnauthorised = errors.New("not logged in to github.com: run gh auth login")

type RateLimitedError struct {
	Until time.Time // zero when github gave no time; wait for the budget's reset
}

func (e *RateLimitedError) Error() string {
	if e.Until.IsZero() {
		return "rate limited"
	}
	return "rate limited until " + e.Until.Local().Format("15:04:05")
}

// PartialError means data came back alongside errors, e.g. from an org
// enforcing SAML. The data has still been decoded.
type PartialError struct {
	Messages []string
}

func (e *PartialError) Error() string {
	return strings.Join(e.Messages, "; ")
}

type Budget struct {
	Cost      int       `json:"cost"`
	Remaining int       `json:"remaining"`
	Limit     int       `json:"limit"`
	ResetAt   time.Time `json:"resetAt"`
}

type Client struct {
	gql *api.GraphQLClient
	now func() time.Time
}

// New finds a token the way gh does: environment, config file, then the
// keyring through gh auth token.
func New() (*Client, error) {
	token, _ := auth.TokenForHost(host)
	if token == "" {
		return nil, ErrUnauthorised
	}
	return newClient(api.ClientOptions{Host: host, AuthToken: token, Timeout: 30 * time.Second})
}

func newClient(opts api.ClientOptions) (*Client, error) {
	gql, err := api.NewGraphQLClient(opts)
	if err != nil {
		return nil, err
	}
	return &Client{gql: gql, now: time.Now}, nil
}

func (c *Client) Do(ctx context.Context, query string, vars map[string]any, out any) error {
	err := c.gql.DoWithContext(ctx, query, vars, out)
	var httpErr *api.HTTPError
	var gqlErr *api.GraphQLError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &httpErr):
		return c.fromHTTP(httpErr)
	case errors.As(err, &gqlErr):
		return fromGraphQL(gqlErr)
	}
	return err
}

func (c *Client) fromHTTP(e *api.HTTPError) error {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorised
	case http.StatusForbidden, http.StatusTooManyRequests:
		if secs, err := strconv.Atoi(e.Headers.Get("Retry-After")); err == nil {
			return &RateLimitedError{Until: c.now().Add(time.Duration(secs) * time.Second)}
		}
		if e.Headers.Get("X-RateLimit-Remaining") == "0" {
			reset, err := strconv.ParseInt(e.Headers.Get("X-RateLimit-Reset"), 10, 64)
			if err != nil {
				return &RateLimitedError{}
			}
			return &RateLimitedError{Until: time.Unix(reset, 0)}
		}
	}
	return e
}

func fromGraphQL(e *api.GraphQLError) error {
	var msgs []string
	for _, item := range e.Errors {
		if item.Type == "RATE_LIMITED" {
			return &RateLimitedError{}
		}
		if !slices.Contains(msgs, item.Message) {
			msgs = append(msgs, item.Message)
		}
	}
	return &PartialError{Messages: msgs}
}
