package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

// People log in through an OpenID Connect provider (Keycloak) with the code flow and PKCE; the core
// is a confidential client. A session is a row in the database, the browser holds its id in an
// HttpOnly cookie. Bearer tokens stay for drivers, CI and agentctl.

const (
	sessionCookie = "charter_session"
	// loginCookie binds a login in progress to the browser that started it: a callback link with
	// someone else's state does not log a victim into the attacker's account
	loginCookie = "charter_login"
	// SessionTTL is how long a session lives without use; every use moves it on.
	SessionTTL = 8 * time.Hour
	// GroupsRefresh is how often a session takes the person's groups from the provider again.
	GroupsRefresh = 15 * time.Minute
	loginTTL      = 10 * time.Minute
	// a session moves its expiry at most this often, not on every request
	touchEvery = time.Minute
)

// Caller is who calls the API: a person logged in through OIDC or a system with a bearer token.
type Caller struct {
	// Name is the person (preferred_username of the provider, the same login as in chat) or the token name.
	Name   string   `json:"person"`
	Kind   string   `json:"kind"`
	Groups []string `json:"groups"`
	// FullName and Email are what the provider says; people only.
	FullName string `json:"name,omitempty"`
	Email    string `json:"email,omitempty"`
}

// Kinds of callers.
const (
	KindPerson = "person"
	KindSystem = "system"
)

// OIDC is the login of people in the browser. The provider is discovered on first use: the core
// starts while the provider is down and logs people in once it is back. With Issuer empty only the
// platform's bearer tokens open a browser session (POST /v1/auth/token): for an installation whose
// provider client is not there yet.
type OIDC struct {
	Issuer, ClientID, ClientSecret string
	// RedirectURL is the callback as the browser sees it, e.g. https://host/charter/v1/auth/callback;
	// its origin is the only origin cookie requests may come from.
	RedirectURL string
	// CookiePath limits the session cookie; empty means the prefix of RedirectURL before /v1/.
	CookiePath string

	mu       sync.Mutex
	client   *http.Client
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	conf     oauth2.Config
	logout   string
}

// Validate checks the settings without calling the provider.
func (o *OIDC) Validate() error {
	if o.Issuer != "" && (o.ClientID == "" || o.ClientSecret == "") {
		return errors.New("OIDC needs the client id and secret with the issuer")
	}
	u, err := url.Parse(o.RedirectURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || !strings.HasSuffix(u.Path, "/v1/auth/callback") {
		return fmt.Errorf("OIDC redirect URL %q must be an absolute http(s) URL ending in /v1/auth/callback", o.RedirectURL)
	}
	if o.CookiePath == "" {
		o.CookiePath = strings.TrimSuffix(u.Path, "v1/auth/callback")
	}
	return nil
}

// origin is scheme://host of the redirect URL.
func (o *OIDC) origin() string {
	u, _ := url.Parse(o.RedirectURL)
	return u.Scheme + "://" + u.Host
}

func (o *OIDC) secure() bool { return strings.HasPrefix(o.RedirectURL, "https://") }

// home is where a login without return_to ends: the UI next to the API.
func (o *OIDC) home() string { return o.CookiePath + "ui/" }

func (o *OIDC) init() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.provider != nil {
		return nil
	}
	if o.Issuer == "" {
		return errors.New("no OIDC provider is configured")
	}
	o.client = &http.Client{Timeout: 10 * time.Second}
	// the provider keeps this context to fetch its keys later: it must outlive the request
	p, err := oidc.NewProvider(oidc.ClientContext(context.Background(), o.client), o.Issuer)
	if err != nil {
		return fmt.Errorf("OIDC provider %s: %w", o.Issuer, err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	_ = p.Claims(&meta)
	o.provider, o.logout = p, meta.EndSession
	o.verifier = p.Verifier(&oidc.Config{ClientID: o.ClientID})
	o.conf = oauth2.Config{ClientID: o.ClientID, ClientSecret: o.ClientSecret, RedirectURL: o.RedirectURL,
		Endpoint: p.Endpoint(), Scopes: []string{oidc.ScopeOpenID, "profile", "email"}}
	return nil
}

// claims are what the core takes from an ID token.
type claims struct {
	Subject  string   `json:"sub"`
	Username string   `json:"preferred_username"`
	Name     string   `json:"name"`
	Email    string   `json:"email"`
	Groups   []string `json:"groups"`
	Nonce    string   `json:"nonce"`
}

// groups strips the leading slash of Keycloak group paths: "/charter-platform" is "charter-platform".
func (c claims) groups() []string {
	out := make([]string, 0, len(c.Groups))
	for _, g := range c.Groups {
		if g = strings.TrimPrefix(g, "/"); g != "" {
			out = append(out, g)
		}
	}
	return out
}

// idToken verifies the ID token of a token response.
func (o *OIDC) idToken(ctx context.Context, tok *oauth2.Token) (claims, string, error) {
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		return claims{}, "", errors.New("no id_token in the token response")
	}
	t, err := o.verifier.Verify(ctx, raw)
	if err != nil {
		return claims{}, "", err
	}
	var c claims
	if err := t.Claims(&c); err != nil {
		return claims{}, "", err
	}
	if c.Username == "" {
		return claims{}, "", errors.New("the ID token has no preferred_username")
	}
	return c, raw, nil
}

// ctx gives the token calls the client with a timeout: a provider that hangs does not hang requests.
func (o *OIDC) ctx(ctx context.Context) context.Context {
	return context.WithValue(ctx, oauth2.HTTPClient, o.client)
}

func random() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func sessionID(cookie string) string {
	h := sha256.Sum256([]byte(cookie))
	return hex.EncodeToString(h[:])
}

// safeReturn keeps return_to a path on this site: an open redirect would hand the login to anyone.
// Browsers drop tabs and newlines from a URL and read a backslash as a slash, so "/<tab>/evil" and
// "/\evil" are "//evil" to them.
func safeReturn(p, home string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.ContainsRune(p, '\\') {
		return home
	}
	for _, r := range p {
		if r < 0x20 || r == 0x7f {
			return home
		}
	}
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "" || u.Host != "" || strings.HasPrefix(u.Path, "//") {
		return home
	}
	return p
}

// login: GET /v1/auth/login?return_to=/path – sends the browser to the provider.
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	o := s.OIDC
	if err := o.init(); err != nil {
		s.Log.Error("oidc", "err", err)
		writeErrCode(w, http.StatusServiceUnavailable, "unavailable", "login provider unavailable")
		return
	}
	state, nonce, verifier := random(), random(), oauth2.GenerateVerifier()
	back := safeReturn(r.URL.Query().Get("return_to"), o.home())
	if _, err := s.Pool.Exec(r.Context(), `INSERT INTO auth_logins (state, verifier, nonce, return_to) VALUES ($1, $2, $3, $4)`,
		state, verifier, nonce, back); err != nil {
		s.Log.Error("oidc login", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: loginCookie, Value: state, Path: o.CookiePath + "v1/auth/", MaxAge: int(loginTTL.Seconds()),
		HttpOnly: true, Secure: o.secure(), SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, o.conf.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), http.StatusFound)
}

// callback: GET /v1/auth/callback?code=&state= – the provider sends the browser back.
func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	o, q := s.OIDC, r.URL.Query()
	if err := o.init(); err != nil {
		s.Log.Error("oidc", "err", err)
		writeErrCode(w, http.StatusServiceUnavailable, "unavailable", "login provider unavailable")
		return
	}
	bound, err := r.Cookie(loginCookie)
	if err != nil || subtle.ConstantTimeCompare([]byte(bound.Value), []byte(q.Get("state"))) != 1 {
		writeErrCode(w, http.StatusBadRequest, "login_expired", "this browser did not start the login, start again")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: loginCookie, Path: o.CookiePath + "v1/auth/", MaxAge: -1, HttpOnly: true, Secure: o.secure()})
	var verifier, nonce, back string
	// a state is used once and only within loginTTL
	err = s.Pool.QueryRow(r.Context(), `DELETE FROM auth_logins WHERE state = $1 AND created_at > $2
		RETURNING verifier, nonce, return_to`, q.Get("state"), time.Now().Add(-loginTTL)).Scan(&verifier, &nonce, &back)
	if errors.Is(err, pgx.ErrNoRows) {
		writeErrCode(w, http.StatusBadRequest, "login_expired", "the login is unknown or expired, start again")
		return
	}
	if err != nil {
		s.Log.Error("oidc callback", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if e := q.Get("error"); e != "" {
		writeErrCode(w, http.StatusForbidden, "login_refused", "the provider refused the login: "+e)
		return
	}
	tok, err := o.conf.Exchange(o.ctx(r.Context()), q.Get("code"), oauth2.VerifierOption(verifier))
	if err != nil {
		s.Log.Warn("oidc exchange", "err", err)
		writeErrCode(w, http.StatusForbidden, "login_refused", "the provider did not accept the login")
		return
	}
	c, raw, err := o.idToken(r.Context(), tok)
	if err == nil && c.Nonce != nonce {
		err = errors.New("nonce mismatch")
	}
	if err != nil {
		s.Log.Warn("oidc id token", "err", err)
		writeErrCode(w, http.StatusForbidden, "login_refused", "the ID token is not valid")
		return
	}
	for _, name := range s.Tokens {
		if name == c.Username { // a person named like a token would get the token's roles and platform
			s.Log.Warn("oidc login refused: the person is named like an API token", "person", c.Username)
			writeErrCode(w, http.StatusForbidden, "login_refused", "this login is taken by a system of the core")
			return
		}
	}
	// a session of this browser before the login ends: one browser, one person
	if old, err := r.Cookie(sessionCookie); err == nil {
		_, _ = s.Pool.Exec(r.Context(), `DELETE FROM sessions WHERE id = $1`, sessionID(old.Value))
	}
	cookie := random()
	if _, err := s.Pool.Exec(r.Context(), `INSERT INTO sessions (id, person, subject, name, email, groups, refresh_token, id_token, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, sessionID(cookie), c.Username, c.Subject, c.Name, c.Email, c.groups(),
		tok.RefreshToken, raw, time.Now().Add(SessionTTL)); err != nil {
		s.Log.Error("oidc session", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.Log.Info("login", "person", c.Username, "groups", c.groups())
	s.setCookie(w, cookie, SessionTTL)
	http.Redirect(w, r, back, http.StatusFound)
}

// logout: POST /v1/auth/logout {"return_to"} → {"logout_url"} – ends the session here; logout_url
// ends it at the provider too and comes back to return_to, the UI goes there. It works without a
// live session as well: a session that ended here may still have one at the provider.
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if !s.OIDC.sameSite(r) {
		writeErrCode(w, http.StatusForbidden, "cross_site", "a logout must come from the UI")
		return
	}
	var req struct {
		ReturnTo string `json:"return_to"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req)
	var idToken, subject string
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = s.Pool.QueryRow(r.Context(), `DELETE FROM sessions WHERE id = $1 RETURNING id_token, subject`, sessionID(c.Value)).
			Scan(&idToken, &subject)
	}
	s.setCookie(w, "", -1)
	out := map[string]any{}
	// a session of a platform token has no provider session to end
	if !strings.HasPrefix(subject, "token:") && s.OIDC.Issuer != "" && s.OIDC.init() == nil && s.OIDC.logout != "" {
		v := url.Values{"client_id": {s.OIDC.ClientID}, "post_logout_redirect_uri": {s.OIDC.origin() + safeReturn(req.ReturnTo, s.OIDC.home())}}
		if idToken != "" {
			v.Set("id_token_hint", idToken)
		}
		out["logout_url"] = s.OIDC.logout + "?" + v.Encode()
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) setCookie(w http.ResponseWriter, value string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: value, Path: s.OIDC.CookiePath, MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: s.OIDC.secure(), SameSite: http.SameSiteLaxMode})
}

// errNoSession means the cookie names no live session.
var errNoSession = errors.New("no session")

// session finds the person of a session cookie. Every GroupsRefresh it takes the groups from the
// provider again by the refresh token. A refusal (invalid_grant: the person was blocked, removed or
// logged out at the provider) or a token for someone else ends the session; a provider that is down
// or answers with an error leaves the session on its last groups and is asked again next time.
func (s *Server) session(w http.ResponseWriter, r *http.Request, cookie string) (Caller, error) {
	return sessionLookup(r.Context(), s.Pool, s.OIDC, s.Log, cookie, func(ttl time.Duration) { s.setCookie(w, cookie, ttl) })
}

func sessionLookup(ctx context.Context, pool *pgxpool.Pool, o *OIDC, log *slog.Logger, cookie string, touched func(time.Duration)) (Caller, error) {
	id := sessionID(cookie)
	c := Caller{Kind: KindPerson}
	var subject, refresh string
	var refreshedAt, seenAt time.Time
	err := pool.QueryRow(ctx, `SELECT person, subject, name, email, groups, refresh_token, refreshed_at, last_seen_at
		FROM sessions WHERE id = $1 AND expires_at > now()`, id).
		Scan(&c.Name, &subject, &c.FullName, &c.Email, &c.Groups, &refresh, &refreshedAt, &seenAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Caller{}, errNoSession
	}
	if err != nil {
		return Caller{}, err
	}
	now := time.Now()
	// a session of a platform token has no refresh token and no groups to take again
	if refresh != "" && now.Sub(refreshedAt) > GroupsRefresh {
		ended, err := refreshSession(ctx, pool, o, id, subject, &c)
		if ended {
			return Caller{}, errNoSession
		}
		if err != nil {
			log.Warn("groups refresh failed, the session keeps its last groups", "person", c.Name, "err", err)
		}
	}
	if now.Sub(seenAt) > touchEvery {
		if _, err := pool.Exec(ctx, `UPDATE sessions SET last_seen_at = now(), expires_at = $2 WHERE id = $1`,
			id, now.Add(SessionTTL)); err != nil {
			return Caller{}, err
		}
		touched(SessionTTL)
	}
	return c, nil
}

// refreshSession takes the groups again under a row lock: requests of one session at once (the
// event stream and a call, several replicas) spend the refresh token once, the others read the result.
func refreshSession(ctx context.Context, pool *pgxpool.Pool, o *OIDC, id, subject string, c *Caller) (ended bool, err error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var refresh string
	var refreshedAt time.Time
	err = tx.QueryRow(ctx, `SELECT name, email, groups, refresh_token, refreshed_at FROM sessions WHERE id = $1 FOR UPDATE`, id).
		Scan(&c.FullName, &c.Email, &c.Groups, &refresh, &refreshedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if time.Since(refreshedAt) <= GroupsRefresh {
		return false, nil // another request has just refreshed
	}
	if err := o.init(); err != nil {
		return false, err
	}
	tok, err := o.conf.TokenSource(o.ctx(ctx), &oauth2.Token{RefreshToken: refresh}).Token()
	if err != nil {
		var re *oauth2.RetrieveError
		// invalid_grant (400) is the provider's no; invalid_client (401) is the core's own misconfiguration
		// and must not log everyone out
		if errors.As(err, &re) && (re.ErrorCode == "invalid_grant" || re.ErrorCode == "" && re.Response != nil && re.Response.StatusCode == http.StatusBadRequest) {
			return true, endSession(ctx, tx, id)
		}
		return false, err
	}
	cl, raw, err := o.idToken(ctx, tok)
	if err == nil && (cl.Subject != subject || cl.Username != c.Name) {
		err = fmt.Errorf("the provider names %q (%s), the session %q (%s)", cl.Username, cl.Subject, c.Name, subject)
	}
	if err != nil {
		return true, endSession(ctx, tx, id)
	}
	c.FullName, c.Email, c.Groups = cl.Name, cl.Email, cl.groups()
	if tok.RefreshToken != "" {
		refresh = tok.RefreshToken
	}
	if _, err := tx.Exec(ctx, `UPDATE sessions SET name = $2, email = $3, groups = $4, refresh_token = $5, id_token = $6,
		refreshed_at = now() WHERE id = $1`, id, c.FullName, c.Email, c.Groups, refresh, raw); err != nil {
		return false, err
	}
	return false, tx.Commit(ctx)
}

// endSession deletes a session in the transaction that holds its row.
func endSession(ctx context.Context, tx pgx.Tx, id string) error {
	if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// unsafeMethod is a request that changes something: a cookie request of that kind must come from the UI.
func unsafeMethod(m string) bool {
	return m != http.MethodGet && m != http.MethodHead && m != http.MethodOptions
}

// sameSite stops cross-site requests made with the session cookie (CSRF): the browser sets Origin
// on such requests and a page of another site cannot set X-Charter-Request without a preflight the
// core never answers.
func (o *OIDC) sameSite(r *http.Request) bool {
	return r.Header.Get("Origin") == o.origin() && r.Header.Get("X-Charter-Request") == "1"
}

// authConfig: GET /v1/auth/config → {"oidc", "token"} – which logins the UI offers.
func (s *Server) authConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"oidc": s.OIDC.Issuer != "", "token": true})
}

// tokenLogin: POST /v1/auth/token with a bearer token of the platform – opens a browser session named
// by the token. It lets the platform into the UI before the provider client exists; the token stays
// out of the page's storage, the session is the same HttpOnly cookie as after an OIDC login.
func (s *Server) tokenLogin(w http.ResponseWriter, r *http.Request) {
	if !s.OIDC.sameSite(r) {
		writeErrCode(w, http.StatusForbidden, "cross_site", "a login must come from the UI")
		return
	}
	tok, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	name := ""
	for t, n := range s.Tokens {
		if subtle.ConstantTimeCompare([]byte(t), []byte(tok)) == 1 {
			name = n
		}
	}
	if name == "" {
		s.Log.Warn("unknown token at token login", "remote", r.RemoteAddr, "forwarded_for", r.Header.Get("X-Forwarded-For"))
		writeErr(w, http.StatusUnauthorized, "unknown token")
		return
	}
	if !s.Platform[name] {
		writeErrCode(w, http.StatusForbidden, "forbidden", "only platform tokens open a browser session")
		return
	}
	cookie := random()
	if _, err := s.Pool.Exec(r.Context(), `INSERT INTO sessions (id, person, subject, expires_at) VALUES ($1, $2, $3, $4)`,
		sessionID(cookie), name, "token:"+name, time.Now().Add(SessionTTL)); err != nil {
		s.Log.Error("token session", "err", err)
		writeErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	s.Log.Info("login with a platform token", "person", name)
	s.setCookie(w, cookie, SessionTTL)
	writeJSON(w, http.StatusOK, map[string]any{"person": name})
}
