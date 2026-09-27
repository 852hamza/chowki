package web

import (
	"crypto/rand"
	"crypto/subtle"
	"embed"
	"encoding/hex"
	"errors"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/852hamza/chowki/internal/auth"
	"github.com/852hamza/chowki/internal/store"
)

//go:embed templates/*.html
var templateFiles embed.FS

//go:embed static
var staticFiles embed.FS

var templates = template.Must(template.ParseFS(templateFiles, "templates/*.html"))

// Cookie names and lifetimes.
const (
	sessionCookie = "chowki_session"
	csrfCookie    = "chowki_csrf"
	sessionTTL    = 12 * time.Hour
	maxSessions   = 1000
)

// UI serves the dashboard under /ui/, for admin tokens: a person signs in
// with one, and a session cookie keeps them signed in.
type UI struct {
	Store  store.Store
	Logger *slog.Logger
	// Now returns the current time; nil means time.Now.
	Now func() time.Time

	mu       sync.Mutex
	sessions map[string]session // by session ID
}

// session is a signed-in person: the prefix of their admin token, which is
// checked again on every request, so that revoking it ends the session.
type session struct {
	admin   string
	expires time.Time
}

// Handler returns the handler of the dashboard.
func (u *UI) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /ui", http.RedirectHandler("/ui/", http.StatusSeeOther))
	mux.HandleFunc("GET /ui/{$}", u.dashboard)
	mux.HandleFunc("GET /ui/login", u.loginPage)
	mux.HandleFunc("POST /ui/login", u.login)
	mux.HandleFunc("POST /ui/logout", u.logout)
	static, _ := fs.Sub(staticFiles, "static") // the directory is embedded
	mux.Handle("GET /ui/static/", http.StripPrefix("/ui/static/", http.FileServerFS(static)))
	return secureHeaders(mux)
}

// secureHeaders keeps the pages to their own origin: no scripts, styles or
// frames from anywhere else.
func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", "default-src 'none'; script-src 'self'; style-src 'self'; img-src 'self'; "+
			"form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		if !strings.HasPrefix(r.URL.Path, "/ui/static/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (u *UI) now() time.Time {
	if u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

func randomID() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b) // crypto/rand.Read never fails
	return hex.EncodeToString(b)
}

// setCookie sets a cookie for the dashboard. It's Secure when the browser
// uses HTTPS, to the gateway or to a proxy in front of it; not always,
// because a browser drops Secure cookies over plain HTTP, which a gateway
// on a local network may use.
func setCookie(w http.ResponseWriter, r *http.Request, name, value string, maxAge time.Duration) {
	secure := r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https"
	//nolint:gosec // Secure over HTTPS only, as above
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/ui", MaxAge: int(maxAge.Seconds()),
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteStrictMode})
}

// csrfToken returns the request's CSRF token, and sets a new one when it
// has none. Forms carry it, and a POST must bring the same value in the
// form and in the cookie.
func csrfToken(w http.ResponseWriter, r *http.Request) string {
	if c, err := r.Cookie(csrfCookie); err == nil && len(c.Value) == 64 {
		return c.Value
	}
	token := randomID()
	setCookie(w, r, csrfCookie, token, sessionTTL)
	return token
}

func validCSRF(r *http.Request) bool {
	c, err := r.Cookie(csrfCookie)
	form := r.PostFormValue("csrf")
	return err == nil && len(c.Value) == 64 && subtle.ConstantTimeCompare([]byte(c.Value), []byte(form)) == 1
}

// signedIn returns the admin token of the request's session, if the session
// is valid and the token isn't revoked.
func (u *UI) signedIn(r *http.Request) (store.AdminToken, bool) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return store.AdminToken{}, false
	}
	u.mu.Lock()
	s, ok := u.sessions[c.Value]
	u.mu.Unlock()
	if !ok || u.now().After(s.expires) {
		return store.AdminToken{}, false
	}
	t, err := u.Store.AdminTokenByPrefix(r.Context(), s.admin)
	if err != nil || t.Revoked() {
		return store.AdminToken{}, false
	}
	return t, true
}

func (u *UI) loginPage(w http.ResponseWriter, r *http.Request) {
	if _, ok := u.signedIn(r); ok {
		http.Redirect(w, r, "/ui/", http.StatusSeeOther)
		return
	}
	u.render(w, http.StatusOK, "login.html", map[string]any{"CSRF": csrfToken(w, r)})
}

func (u *UI) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	if !validCSRF(r) {
		u.render(w, http.StatusForbidden, "login.html", map[string]any{"CSRF": csrfToken(w, r),
			"Error": "The form expired. Sign in again."})
		return
	}
	t, err := auth.VerifyAdmin(r.Context(), u.Store, strings.TrimSpace(r.PostFormValue("token")))
	if err != nil {
		if !errors.Is(err, auth.ErrInvalid) && !errors.Is(err, auth.ErrRevoked) {
			u.Logger.Error("dashboard sign-in", "error", err)
		}
		u.Logger.Warn("dashboard sign-in failed")
		u.render(w, http.StatusUnauthorized, "login.html", map[string]any{"CSRF": csrfToken(w, r),
			"Error": "That admin token isn't valid. Create one with chowki admin create."})
		return
	}
	id := randomID()
	now := u.now()
	u.mu.Lock()
	if u.sessions == nil {
		u.sessions = map[string]session{}
	}
	for sid, s := range u.sessions {
		if now.After(s.expires) || len(u.sessions) >= maxSessions {
			delete(u.sessions, sid)
		}
	}
	u.sessions[id] = session{admin: t.Prefix, expires: now.Add(sessionTTL)}
	u.mu.Unlock()
	setCookie(w, r, sessionCookie, id, sessionTTL)
	u.Logger.Info("dashboard sign-in", "admin", t.Prefix)
	http.Redirect(w, r, "/ui/", http.StatusSeeOther)
}

func (u *UI) logout(w http.ResponseWriter, r *http.Request) {
	if validCSRF(r) {
		if c, err := r.Cookie(sessionCookie); err == nil {
			u.mu.Lock()
			delete(u.sessions, c.Value)
			u.mu.Unlock()
		}
		setCookie(w, r, sessionCookie, "", -time.Second)
	}
	http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
}

func (u *UI) dashboard(w http.ResponseWriter, r *http.Request) {
	if _, ok := u.signedIn(r); !ok {
		http.Redirect(w, r, "/ui/login", http.StatusSeeOther)
		return
	}
	p, err := u.build(r.Context(), r.URL.Query())
	if err != nil {
		u.Logger.Error("dashboard", "error", err)
		http.Error(w, "The dashboard couldn't be built; the gateway's log has the details.", http.StatusInternalServerError)
		return
	}
	p.CSRF = csrfToken(w, r)
	u.render(w, http.StatusOK, "dashboard.html", p)
}

func (u *UI) render(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := templates.ExecuteTemplate(w, name, data); err != nil {
		u.Logger.Error("render dashboard page", "page", name, "error", err)
	}
}
