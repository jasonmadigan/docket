package web

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"html/template"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jasonmadigan/docket/internal/browser"
	"github.com/jasonmadigan/docket/internal/engine"
)

//go:embed templates static
var files embed.FS

type Engine interface {
	Current() engine.State
	Subscribe() (<-chan engine.State, func())
}

type Options struct {
	Addr      string
	Open      bool
	Log       io.Writer
	Location  *time.Location
	KeepAlive time.Duration // how often an idle event stream sends a comment
}

type Server struct {
	eng       Engine
	tmpl      *template.Template
	loc       *time.Location
	keepAlive time.Duration
}

func New(eng Engine, opts Options) (*Server, error) {
	if opts.Location == nil {
		opts.Location = time.Local
	}
	if opts.KeepAlive <= 0 {
		opts.KeepAlive = 25 * time.Second
	}
	tmpl, err := template.New("").Funcs(template.FuncMap{"lower": strings.ToLower}).ParseFS(files, "templates/*.html")
	if err != nil {
		return nil, err
	}
	return &Server{eng: eng, tmpl: tmpl, loc: opts.Location, keepAlive: opts.KeepAlive}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.render("page"))
	mux.HandleFunc("GET /sections", s.render("sections"))
	mux.HandleFunc("GET /events", s.events)
	mux.Handle("GET /static/{file}", http.FileServerFS(files))
	return guard(mux)
}

func (s *Server) render(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		if err := s.tmpl.ExecuteTemplate(&buf, name, view(s.eng.Current(), s.loc)); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = buf.WriteTo(w)
	}
}

// events announces each new state; the page then fetches /sections. The
// state current at subscription is announced too, covering anything that
// landed between the page loading and the stream opening.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	states, unsubscribe := s.eng.Subscribe()
	defer unsubscribe()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	if err := rc.Flush(); err != nil {
		return
	}
	tick := time.NewTicker(s.keepAlive)
	defer tick.Stop()
	for {
		var msg string
		select {
		case <-r.Context().Done():
			return
		case <-states:
			msg = "event: snapshot\ndata: new\n\n"
		case <-tick.C:
			msg = ": keepalive\n\n"
		}
		if _, err := io.WriteString(w, msg); err != nil {
			return
		}
		if err := rc.Flush(); err != nil {
			return
		}
	}
}

// guard refuses hostnames other than localhost, which defeats dns
// rebinding; ip literals pass so an explicit --addr works from the lan.
func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !allowedHost(r.Host) {
			http.Error(w, "host not allowed", http.StatusForbidden)
			return
		}
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("Cache-Control", "no-store")
		next.ServeHTTP(w, r)
	})
}

func allowedHost(hostport string) bool {
	host := hostport
	if h, _, err := net.SplitHostPort(hostport); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.TrimPrefix(host, "["), "]")
	return strings.EqualFold(host, "localhost") || net.ParseIP(host) != nil
}

func Run(ctx context.Context, eng Engine, opts Options) error {
	if opts.Log == nil {
		opts.Log = io.Discard
	}
	s, err := New(eng, opts)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", opts.Addr)
	if err != nil {
		return err
	}
	addr := ln.Addr().(*net.TCPAddr)
	if !addr.IP.IsLoopback() {
		fmt.Fprintf(opts.Log, "warning: listening on %s: there's no auth, and private repo titles are on the page\n", addr)
	}
	link := "http://" + reachable(addr)
	fmt.Fprintf(opts.Log, "docket: serving %s\n", link)
	srv := &http.Server{
		Handler:           s.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	if opts.Open {
		go func() {
			if err := browser.Open(link); err != nil {
				fmt.Fprintln(opts.Log, "docket:", err)
			}
		}()
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdown, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		return srv.Shutdown(shutdown)
	}
}

// reachable swaps an unspecified address for localhost so the printed
// link works.
func reachable(a *net.TCPAddr) string {
	if a.IP.IsUnspecified() {
		return net.JoinHostPort("localhost", strconv.Itoa(a.Port))
	}
	return a.String()
}
