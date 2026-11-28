package api_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/google/uuid"

	"github.com/charterhq/charter/internal/api"
	"github.com/charterhq/charter/internal/run"
)

// idp is a fake OpenID Connect provider: it logs in whoever the test names, with no login page.
type idp struct {
	srv    *httptest.Server
	key    *rsa.PrivateKey
	mu     sync.Mutex
	user   string
	groups map[string][]string
	codes  map[string]grant
	refuse bool // refresh tokens are refused (the person was blocked)
}

type grant struct{ user, nonce, challenge string }

func newIDP(t *testing.T) *idp {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	p := &idp{key: key, groups: map[string][]string{}, codes: map[string]grant{}}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		u := p.srv.URL
		_ = json.NewEncoder(w).Encode(map[string]any{"issuer": u, "authorization_endpoint": u + "/auth", "token_endpoint": u + "/token",
			"jwks_uri": u + "/jwks", "end_session_endpoint": u + "/logout", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("client_id") != "charter" {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		code := uuid.NewString()
		p.codes[code] = grant{user: p.user, nonce: q.Get("nonce"), challenge: q.Get("code_challenge")}
		p.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+url.Values{"code": {code}, "state": {q.Get("state")}}.Encode(), http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, ok := r.BasicAuth()
		if !ok {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
		}
		if id != "charter" || secret != "s3cret" {
			http.Error(w, `{"error":"invalid_client"}`, http.StatusUnauthorized)
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		var user, nonce string
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			g, ok := p.codes[r.PostForm.Get("code")]
			delete(p.codes, r.PostForm.Get("code"))
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if !ok || base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			user, nonce = g.user, g.nonce
		case "refresh_token":
			if p.refuse {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			user = strings.TrimPrefix(r.PostForm.Get("refresh_token"), "rt-")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "expires_in": 300,
			"refresh_token": "rt-" + user, "id_token": p.idToken(t, user, nonce)})
	})
	p.srv = httptest.NewServer(mux)
	t.Cleanup(p.srv.Close)
	return p
}

func (p *idp) idToken(t *testing.T, user, nonce string) string {
	groups := []string{}
	for _, g := range p.groups[user] {
		groups = append(groups, "/"+g) // Keycloak with full paths
	}
	c := map[string]any{"iss": p.srv.URL, "sub": "sub-" + user, "aud": "charter", "exp": time.Now().Add(5 * time.Minute).Unix(),
		"iat": time.Now().Unix(), "preferred_username": user, "name": strings.ToUpper(user), "email": user + "@example.com", "groups": groups}
	if nonce != "" {
		c["nonce"] = nonce
	}
	b, _ := json.Marshal(c)
	sig, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: p.key, KeyID: "k1"}}, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		t.Fatal(err)
	}
	jws, err := sig.Sign(b)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := jws.CompactSerialize()
	return raw
}

// withOIDC serves the API of e with people logging in through p.
func withOIDC(t *testing.T, e env, p *idp) env {
	t.Helper()
	var h http.Handler
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { h.ServeHTTP(w, r) }))
	t.Cleanup(srv.Close)
	s := &api.Server{Pool: e.s.Pool, Engine: e.s.Engine, Tokens: e.s.Tokens, Formats: e.s.Formats, Platform: e.s.Platform,
		PlatformGroup: "charter-platform", Log: slog.Default(),
		OIDC: &api.OIDC{Issuer: p.srv.URL, ClientID: "charter", ClientSecret: "s3cret", RedirectURL: srv.URL + "/v1/auth/callback"}}
	if err := s.OIDC.Validate(); err != nil {
		t.Fatal(err)
	}
	h = s.Handler()
	t.Cleanup(s.StopStreams)
	e.srv, e.s = srv, s
	return e
}

// browser is a person's browser: it keeps the cookies.
type browser struct {
	c   *http.Client
	e   env
	hop []string // where the last login went
}

func login(t *testing.T, e env, p *idp, user, returnTo string) *browser {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	b := &browser{e: e}
	b.c = &http.Client{Jar: jar, CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		b.hop = append(b.hop, r.URL.String())
		return nil
	}}
	p.mu.Lock()
	p.user = user
	p.mu.Unlock()
	resp, err := b.c.Get(e.srv.URL + "/v1/auth/login?return_to=" + url.QueryEscape(returnTo))
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return b
}

// do sends a request with the cookie; ui adds what the UI sends against CSRF.
func (b *browser) do(t *testing.T, method, path string, body any, ui bool) (int, map[string]any) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, b.e.srv.URL+path, rd)
	if ui {
		req.Header.Set("Origin", b.e.srv.URL)
		req.Header.Set("X-Charter-Request", "1")
	}
	resp, err := b.c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func TestOIDCLogin(t *testing.T) {
	p := newIDP(t)
	p.groups["jane.doe"] = []string{"agent-builders"}
	p.groups["pat.lee"] = []string{"charter-platform"}
	base, _ := setupObjects(t)
	e := withOIDC(t, base, p)

	jane := login(t, e, p, "jane.doe", "/ui/drafts")
	if last := jane.hop[len(jane.hop)-1]; last != e.srv.URL+"/ui/drafts" {
		t.Fatalf("login ends at %s", last)
	}
	code, me := jane.do(t, "GET", "/v1/me", nil, false)
	if code != http.StatusOK || me["person"] != "jane.doe" || me["kind"] != "person" || me["platform"] != false ||
		me["name"] != "JANE.DOE" || len(me["groups"].([]any)) != 1 || me["groups"].([]any)[0] != "agent-builders" {
		t.Fatalf("me: %d %v", code, me)
	}
	var stored string
	_ = e.pool.QueryRow(context.Background(), `SELECT id FROM sessions WHERE person = 'jane.doe'`).Scan(&stored)
	for _, c := range jane.c.Jar.Cookies(mustURL(e.srv.URL)) {
		if c.Value == stored {
			t.Fatal("the database keeps the cookie value itself")
		}
	}

	// the platform comes from the group
	if _, me := login(t, e, p, "pat.lee", "/").do(t, "GET", "/v1/me", nil, false); me["platform"] != true {
		t.Fatalf("pat: %v", me)
	}
	// bearer tokens still work and are systems
	if code, me := call(t, e, "GET", "/v1/me", "alice-token", nil); code != http.StatusOK || me["kind"] != "system" {
		t.Fatalf("bearer: %d %v", code, me)
	}
	// no cookie, no token
	if code, out := call(t, e, "GET", "/v1/me", "", nil); code != http.StatusUnauthorized || out["code"] != "login_required" {
		t.Fatalf("anonymous: %d %v", code, out)
	}

	// a change with the cookie must come from the UI
	obj := map[string]any{"connection": "platform", "title": "draft"}
	if code, out := jane.do(t, "POST", "/v1/agents/builder/objects", obj, false); code != http.StatusForbidden || out["code"] != "cross_site" {
		t.Fatalf("cross-site: %d %v", code, out)
	}
	code, created := jane.do(t, "POST", "/v1/agents/builder/objects", obj, true)
	if code != http.StatusCreated {
		t.Fatalf("create: %d %v", code, created)
	}

	// endpoints of drivers and pipelines trust the caller's word: not for people
	msg := map[string]any{"conversation": "c", "message_id": "m", "author": "someone.else", "text": "hi"}
	if code, out := jane.do(t, "POST", "/v1/conversations/builder/platform/requests", msg, true); code != http.StatusForbidden {
		t.Fatalf("converse as a person: %d %v", code, out)
	}
	if code, _ := jane.do(t, "GET", "/v1/runs", nil, false); code != http.StatusForbidden {
		t.Fatalf("runs as a person: %d", code)
	}

	// logout ends the session here and points at the provider's logout
	code, out := jane.do(t, "POST", "/v1/auth/logout", nil, true)
	if code != http.StatusOK || !strings.HasPrefix(out["logout_url"].(string), p.srv.URL+"/logout?") {
		t.Fatalf("logout: %d %v", code, out)
	}
	if code, _ := jane.do(t, "GET", "/v1/me", nil, false); code != http.StatusUnauthorized {
		t.Fatalf("after logout: %d", code)
	}
}

func mustURL(s string) *url.URL { u, _ := url.Parse(s); return u }

func TestOIDCLoginChecks(t *testing.T) {
	p := newIDP(t)
	base, _ := setupObjects(t)
	e := withOIDC(t, base, p)

	// return_to stays on this site
	for _, back := range []string{"//evil.example/x", "https://evil.example/", `/\evil.example`} {
		b := login(t, e, p, "jane.doe", back)
		if last := b.hop[len(b.hop)-1]; last != e.srv.URL+"/ui/" {
			t.Fatalf("return_to %q ends at %s", back, last)
		}
	}

	// a state works once
	jar, _ := cookiejar.New(nil)
	var callback string
	c := &http.Client{Jar: jar, CheckRedirect: func(r *http.Request, _ []*http.Request) error {
		if r.URL.Path == "/v1/auth/callback" {
			callback = r.URL.String()
			return http.ErrUseLastResponse
		}
		return nil
	}}
	p.user = "jane.doe"
	resp, err := c.Get(e.srv.URL + "/v1/auth/login")
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for i, want := range []int{http.StatusFound, http.StatusBadRequest} {
		req, _ := http.NewRequest("GET", callback, nil)
		resp, err := http.DefaultTransport.RoundTrip(req)
		if err != nil {
			t.Fatal(err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("callback %d: %d", i, resp.StatusCode)
		}
	}
	// a made-up state
	if resp, _ := http.Get(e.srv.URL + "/v1/auth/callback?code=x&state=nope"); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown state: %d", resp.StatusCode)
	}
}

func TestOIDCGroupsRefresh(t *testing.T) {
	p := newIDP(t)
	p.groups["jane.doe"] = []string{"charter-platform"}
	base, _ := setupObjects(t)
	e := withOIDC(t, base, p)
	jane := login(t, e, p, "jane.doe", "/")
	if _, me := jane.do(t, "GET", "/v1/me", nil, false); me["platform"] != true {
		t.Fatalf("before: %v", me)
	}
	stale := func() {
		if _, err := e.pool.Exec(context.Background(), `UPDATE sessions SET refreshed_at = now() - interval '1 hour'`); err != nil {
			t.Fatal(err)
		}
	}
	// the person left the group: the session takes that from the provider
	p.mu.Lock()
	p.groups["jane.doe"] = nil
	p.mu.Unlock()
	if _, me := jane.do(t, "GET", "/v1/me", nil, false); me["platform"] != true {
		t.Fatalf("groups refreshed too early: %v", me)
	}
	stale()
	if _, me := jane.do(t, "GET", "/v1/me", nil, false); me["platform"] != false || len(me["groups"].([]any)) != 0 {
		t.Fatalf("after refresh: %v", me)
	}
	// the provider refuses the refresh: the session ends
	p.mu.Lock()
	p.refuse = true
	p.mu.Unlock()
	stale()
	if code, _ := jane.do(t, "GET", "/v1/me", nil, false); code != http.StatusUnauthorized {
		t.Fatalf("refused refresh: %d", code)
	}
	var n int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FROM sessions`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d sessions left", n)
	}
}

func TestTriggerGroups(t *testing.T) {
	p := newIDP(t)
	p.groups["jane.doe"] = []string{"agent-builders"}
	base, _ := setupObjectsWith(t, []string{"agent-builders"}, `{"reply":"ok"}`, `{"reply":"ok"}`)
	e := withOIDC(t, base, p)
	jane, john := login(t, e, p, "jane.doe", "/"), login(t, e, p, "john.roe", "/")

	say := func(b *browser) int {
		t.Helper()
		code, obj := b.do(t, "POST", "/v1/agents/builder/objects", map[string]any{"connection": "platform"}, true)
		if code != http.StatusCreated {
			t.Fatalf("create: %d %v", code, obj)
		}
		id := obj["id"].(string)
		code, conv := b.do(t, "POST", "/v1/objects/"+id+"/conversations", map[string]any{}, true)
		if code != http.StatusCreated {
			t.Fatalf("conversation: %d %v", code, conv)
		}
		cid := conv["conversation"].(map[string]any)["id"].(string)
		code, _ = b.do(t, "POST", "/v1/objects/"+id+"/conversations/"+cid+"/messages", map[string]any{"message_id": "m1", "text": "hi"}, true)
		return code
	}
	if code := say(jane); code != http.StatusAccepted {
		t.Fatalf("member of the group: %d", code)
	}
	if code := say(john); code != http.StatusForbidden {
		t.Fatalf("outside the group: %d", code)
	}
}

func TestResolveAsPerson(t *testing.T) {
	scn := run.Scenario{Name: "triage", Level: 1, ModelClass: "c", Autonomy: "act", Action: "jira.create_issue",
		Approval: &run.Approval{Timeout: run.Duration(time.Hour), OnTimeout: "cancel"}}
	p := newIDP(t)
	e := withOIDC(t, setup(t, scn, `{"summary":"x"}`), p)
	_, out := launch(t, e, "kestra-token")
	runID := out["runs"].([]any)[0].(map[string]any)["run_id"].(string)
	var wait, fp string
	for i := 0; i < 200 && wait == ""; i++ {
		if r, ok := e.ch.LastApproval(); ok {
			wait, fp = r.WaitID, r.Fingerprint
		}
		time.Sleep(50 * time.Millisecond)
	}
	path, body := "/v1/waits/"+wait+"/resolve", map[string]any{"decision": "approve", "fingerprint": fp}
	jane, john := login(t, e, p, "jane.doe", "/"), login(t, e, p, "john.roe", "/")

	// a wait without an object is answered in its chat, not by a person of the UI
	if code, out := jane.do(t, "POST", path, body, true); code != http.StatusForbidden || out["code"] != "forbidden" {
		t.Fatalf("no object: %d %v", code, out)
	}
	// the run is about an object jane owns: jane answers, john does not
	ctx := context.Background()
	oid := uuid.New()
	if _, err := e.pool.Exec(ctx, `INSERT INTO objects (id, agent, connection, key, agent_version_id, created_by)
		SELECT $1, agent, connection, 'k', agent_version_id, 'jane.doe' FROM runs WHERE id = $2`, oid, runID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `INSERT INTO object_members (object_id, person, role, added_by) VALUES
		($1, 'jane.doe', 'owner', 'test'), ($1, 'john.roe', 'member', 'test')`, oid); err != nil {
		t.Fatal(err)
	}
	if _, err := e.pool.Exec(ctx, `UPDATE runs SET object_id = $1 WHERE id = $2`, oid, runID); err != nil {
		t.Fatal(err)
	}
	if code, _ := john.do(t, "POST", path, body, true); code != http.StatusForbidden {
		t.Fatalf("member: %d", code)
	}
	if code, out := jane.do(t, "POST", path, body, true); code != http.StatusOK || out["by"] != "jane.doe" {
		t.Fatalf("owner: %d %v", code, out)
	}
	if status := waitStatus(t, e, runID); status != run.StatusOK {
		t.Fatalf("status %s", status)
	}
}
