package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"mime"
	"net"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jasonmadigan/docket/internal/archive"
	"github.com/jasonmadigan/docket/internal/browser"
	"github.com/jasonmadigan/docket/internal/config"
	"github.com/jasonmadigan/docket/internal/engine"
	"github.com/jasonmadigan/docket/internal/model"
)

//go:embed templates static
var files embed.FS

type Engine interface {
	Current() engine.State
	Subscribe() (<-chan engine.State, func())
}

// Settings reads and changes docket's settings; config.Store in practice.
type Settings interface {
	Get() config.Config
	Set(config.Config) error
	Path() string
}

// Archiver hides items locally; archive.Store in practice.
type Archiver interface {
	Archive(id, ref, title string) error
	Unarchive(id string) error
}

type Options struct {
	Addr      string
	Open      bool
	Log       io.Writer
	Location  *time.Location
	KeepAlive time.Duration // how often an idle event stream sends a comment
	Settings  Settings      // nil hides the settings
	Archive   Archiver      // nil hides archiving
}

type Server struct {
	eng       Engine
	tmpl      *template.Template
	loc       *time.Location
	keepAlive time.Duration
	settings  Settings
	archive   Archiver
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
	return &Server{eng: eng, tmpl: tmpl, loc: opts.Location, keepAlive: opts.KeepAlive, settings: opts.Settings, archive: opts.Archive}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.render("page"))
	mux.HandleFunc("GET /sections", s.render("sections"))
	mux.HandleFunc("GET /events", s.events)
	if s.settings != nil {
		mux.HandleFunc("GET /settings", s.getSettings)
		mux.HandleFunc("POST /settings", s.postSettings)
	}
	if s.archive != nil {
		mux.HandleFunc("POST /archive", s.postArchive)
	}
	mux.Handle("GET /static/{file}", http.FileServerFS(files))
	return guard(mux)
}

func (s *Server) render(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		var buf bytes.Buffer
		v := view(s.eng.Current(), s.loc, s.archive != nil)
		if s.settings != nil {
			v.Settings, v.SettingsPath = true, s.settings.Path()
		}
		if err := s.tmpl.ExecuteTemplate(&buf, name, v); err != nil {
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

type settingsJSON struct {
	Poll         string   `json:"poll"`
	IgnoreActors []string `json:"ignore_actors"`
	Choices      []string `json:"choices,omitempty"`
}

func (s *Server) getSettings(w http.ResponseWriter, _ *http.Request) {
	c := s.settings.Get()
	out := settingsJSON{Poll: config.FormatPoll(c.Poll), IgnoreActors: c.IgnoreActors}
	if out.IgnoreActors == nil {
		out.IgnoreActors = []string{}
	}
	for _, d := range config.PollChoices {
		out.Choices = append(out.Choices, config.FormatPoll(d))
	}
	if !slices.Contains(out.Choices, out.Poll) {
		out.Choices = append(out.Choices, out.Poll)
	}
	writeJSON(w, http.StatusOK, out)
}

// foreign refuses requests another site made, and any not in json: a form
// posted from elsewhere can't set that content type, and browsers label
// cross-site requests.
func foreign(w http.ResponseWriter, r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site request"})
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "cross-origin request"})
		return true
	}
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeJSON(w, http.StatusUnsupportedMediaType, map[string]string{"error": "send json"})
		return true
	}
	return false
}

// postSettings takes json from this page only.
func (s *Server) postSettings(w http.ResponseWriter, r *http.Request) {
	if foreign(w, r) {
		return
	}
	var in settingsJSON
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	poll, err := time.ParseDuration(in.Poll)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "refresh interval: " + err.Error()})
		return
	}
	if err := s.settings.Set(config.Config{Poll: poll, IgnoreActors: in.IgnoreActors}); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

var errNotShown = errors.New("not in docket's lists")

// postArchive archives or unarchives one item, taking json from this page
// only.
func (s *Server) postArchive(w http.ResponseWriter, r *http.Request) {
	if foreign(w, r) {
		return
	}
	var in struct {
		ID       string `json:"id"`
		Archived bool   `json:"archived"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || in.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	var err error
	if in.Archived {
		err = s.archiveShown(in.ID)
	} else {
		err = s.archive.Unarchive(in.ID)
	}
	switch {
	case err == nil:
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, errNotShown), errors.Is(err, archive.ErrNotArchived):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
}

// archiveShown archives an item the page lists; one archived already is
// left as it is.
func (s *Server) archiveShown(id string) error {
	snap := s.eng.Current().Snapshot
	if _, ok := snap.Archived.Find(id); ok {
		return nil
	}
	for _, l := range []model.List{snap.PRs, snap.Issues} {
		if row, ok := l.Find(id); ok {
			it := row.Item()
			return s.archive.Archive(it.ID, it.Ref(), it.Title)
		}
	}
	return fmt.Errorf("%s: %w", id, errNotShown)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
