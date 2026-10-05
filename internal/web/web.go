// Package web serves the browser UI: login, map, stats, visits, trips,
// imports/exports and settings. Pages are server-rendered shells that talk to
// the /api/v1 JSON API with the signed-in user's API key.
package web

import (
	"crypto/hmac"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/sixface/beenthere/internal/httpx"
	"github.com/sixface/beenthere/internal/jobs"
	"github.com/sixface/beenthere/internal/store"
)

//go:embed templates/*.html static
var assets embed.FS

const (
	cookieName = "bt_session"
	sessionTTL = 14 * 24 * time.Hour
)

type Web struct {
	S      *store.Store
	Jobs   *jobs.Manager
	Log    *slog.Logger
	secret []byte
	pages  map[string]*template.Template

	mu       sync.Mutex
	attempts map[string][]time.Time
}

func New(s *store.Store, j *jobs.Manager, log *slog.Logger, secret string) *Web {
	w := &Web{S: s, Jobs: j, Log: log, secret: []byte(secret), pages: map[string]*template.Template{}, attempts: map[string][]time.Time{}}
	names, _ := fs.Glob(assets, "templates/*.html")
	for _, n := range names {
		base := strings.TrimSuffix(strings.TrimPrefix(n, "templates/"), ".html")
		if base == "layout" {
			continue
		}
		w.pages[base] = template.Must(template.New("").Funcs(template.FuncMap{"navItems": navItems}).ParseFS(assets, "templates/layout.html", n))
	}
	return w
}

type navItem struct{ Key, Path, Label string }

func navItems() []navItem {
	return []navItem{{"map", "/map", "Map"}, {"stats", "/stats", "Stats"}, {"visits", "/visits", "Visits"},
		{"trips", "/trips", "Trips"}, {"places", "/places", "Places"}, {"imports", "/imports", "Imports"},
		{"exports", "/exports", "Exports"}, {"settings", "/settings", "Settings"}}
}

type view struct {
	Title  string
	Active string
	User   *store.User
	Error  string
	Flash  string
	Data   any
}

func (w *Web) render(rw http.ResponseWriter, page string, v view) {
	rw.Header().Set("Content-Type", "text/html; charset=utf-8")
	rw.Header().Set("Cache-Control", "no-store")
	rw.Header().Set("X-Frame-Options", "DENY")
	rw.Header().Set("Referrer-Policy", "same-origin")
	if err := w.pages[page].ExecuteTemplate(rw, "layout", v); err != nil {
		w.Log.Error("render", "page", page, "err", err)
	}
}

// ---- sessions ----

func (w *Web) sign(payload string) string {
	m := hmac.New(sha256.New, w.secret)
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + hex.EncodeToString(m.Sum(nil))
}

func pwFingerprint(u *store.User) string {
	h := sha256.Sum256([]byte(u.EncryptedPass))
	return hex.EncodeToString(h[:4])
}

func (w *Web) setSession(rw http.ResponseWriter, r *http.Request, u *store.User) {
	exp := time.Now().Add(sessionTTL).Unix()
	http.SetCookie(rw, &http.Cookie{
		Name: cookieName, Value: w.sign(fmt.Sprintf("%d|%d|%s", u.ID, exp, pwFingerprint(u))),
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https",
		MaxAge: int(sessionTTL.Seconds()),
	})
}

func (w *Web) currentUser(r *http.Request) *store.User {
	c, err := r.Cookie(cookieName)
	if err != nil {
		return nil
	}
	parts := strings.SplitN(c.Value, ".", 2)
	if len(parts) != 2 {
		return nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil
	}
	if !hmac.Equal([]byte(w.sign(string(raw))), []byte(c.Value)) {
		return nil
	}
	f := strings.Split(string(raw), "|")
	if len(f) != 3 {
		return nil
	}
	exp, _ := strconv.ParseInt(f[1], 10, 64)
	id, _ := strconv.ParseInt(f[0], 10, 64)
	if time.Now().Unix() > exp {
		return nil
	}
	u, err := w.S.UserByID(r.Context(), id)
	if err != nil || pwFingerprint(u) != f[2] || !u.Active() {
		return nil
	}
	return u
}

type pageHandler func(http.ResponseWriter, *http.Request, *store.User)

func (w *Web) authed(h pageHandler) http.HandlerFunc {
	return func(rw http.ResponseWriter, r *http.Request) {
		u := w.currentUser(r)
		if u == nil {
			http.Redirect(rw, r, "/login", http.StatusSeeOther)
			return
		}
		h(rw, r, u)
	}
}

func clientIP(r *http.Request) string {
	if h, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return h
	}
	return r.RemoteAddr
}

func (w *Web) throttled(ip string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	cut := time.Now().Add(-10 * time.Minute)
	kept := w.attempts[ip][:0]
	for _, t := range w.attempts[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	w.attempts[ip] = kept
	return len(kept) >= 10
}

func (w *Web) noteFailure(ip string) {
	w.mu.Lock()
	w.attempts[ip] = append(w.attempts[ip], time.Now())
	w.mu.Unlock()
}

func (w *Web) login(rw http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		if w.currentUser(r) != nil {
			http.Redirect(rw, r, "/map", http.StatusSeeOther)
			return
		}
		w.render(rw, "login", view{Title: "Sign in"})
		return
	}
	ip := clientIP(r)
	if w.throttled(ip) {
		rw.WriteHeader(http.StatusTooManyRequests)
		w.render(rw, "login", view{Title: "Sign in", Error: "Too many attempts. Try again in a few minutes."})
		return
	}
	email, pw := strings.TrimSpace(r.FormValue("email")), r.FormValue("password")
	u, err := w.S.UserByEmail(r.Context(), email)
	// Always run a bcrypt comparison to keep timing uniform.
	ok := false
	if err == nil {
		ok = u.CheckPassword(pw) && u.Active()
	} else {
		(&store.User{EncryptedPass: dummyHash}).CheckPassword(pw)
	}
	if !ok {
		w.noteFailure(ip)
		rw.WriteHeader(http.StatusUnauthorized)
		w.render(rw, "login", view{Title: "Sign in", Error: "Invalid email or password."})
		return
	}
	if u.OTPRequired {
		rw.WriteHeader(http.StatusUnauthorized)
		w.render(rw, "login", view{Title: "Sign in", Error: "Two-factor accounts must sign in through the Rails app (2FA is not supported yet)."})
		return
	}
	_ = w.S.TouchSignIn(r.Context(), u.ID, ip)
	w.setSession(rw, r, u)
	http.Redirect(rw, r, "/map", http.StatusSeeOther)
}

// dummyHash is a real bcrypt hash used to equalize timing for unknown users.
var dummyHash = func() string {
	h, _ := bcrypt.GenerateFromPassword([]byte("timing-equalizer"), 12)
	return string(h)
}()

func (w *Web) logout(rw http.ResponseWriter, r *http.Request) {
	http.SetCookie(rw, &http.Cookie{Name: cookieName, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	http.Redirect(rw, r, "/login", http.StatusSeeOther)
}

func (w *Web) Routes(mux *http.ServeMux) {
	static, _ := fs.Sub(assets, "static")
	fileServer := http.StripPrefix("/static/", http.FileServer(http.FS(static)))
	mux.HandleFunc("GET /static/", func(rw http.ResponseWriter, r *http.Request) {
		rw.Header().Set("Cache-Control", "public, max-age=86400")
		if strings.HasSuffix(r.URL.Path, ".mjs") {
			rw.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		}
		fileServer.ServeHTTP(rw, r)
	})
	mux.HandleFunc("GET /{$}", func(rw http.ResponseWriter, r *http.Request) {
		if w.currentUser(r) != nil {
			http.Redirect(rw, r, "/map", http.StatusSeeOther)
			return
		}
		http.Redirect(rw, r, "/login", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /login", w.login)
	mux.HandleFunc("POST /login", w.login)
	mux.HandleFunc("POST /logout", w.logout)

	for _, p := range []struct{ path, page, title string }{
		{"/map", "map", "Map"}, {"/stats", "stats", "Stats"}, {"/visits", "visits", "Visits"},
		{"/trips", "trips", "Trips"}, {"/places", "places", "Places"}, {"/imports", "imports", "Imports"},
		{"/exports", "exports", "Exports"}, {"/settings", "settings", "Settings"},
	} {
		p := p
		mux.HandleFunc("GET "+p.path, w.authed(func(rw http.ResponseWriter, r *http.Request, u *store.User) {
			w.render(rw, p.page, view{Title: p.title, Active: p.page, User: u})
		}))
	}
	_ = httpx.Version
}
