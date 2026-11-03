package httpapi

import (
	"bytes"
	"context"
	"easycr/internal/auth"
	"easycr/internal/config"
	"easycr/internal/registry"
	"easycr/internal/state"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

type client struct {
	h                      http.Handler
	cookie                 *http.Cookie
	csrf, password, origin string
}

func setup(t *testing.T, https bool) (*client, auth.Service) {
	t.Helper()
	st, err := state.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	var log bytes.Buffer
	if err = auth.EnsureBootstrap(st, &log); err != nil {
		t.Fatal(err)
	}
	svc := auth.Service{Store: st}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	engine, err := registry.NewEngine(ctx, t.TempDir(), svc)
	if err != nil {
		t.Fatal(err)
	}
	origin := "http://localhost:5000"
	if https {
		origin = "https://registry.test"
	}
	h, err := New(config.Config{PublicURL: origin}, svc, engine)
	if err != nil {
		t.Fatal(err)
	}
	return &client{h: h, password: strings.TrimSpace(strings.Split(log.String(), "password=")[1]), origin: origin}, svc
}
func (c *client) request(method, path, body string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.RemoteAddr = "127.0.0.1:1234"
	if c.cookie != nil {
		r.AddCookie(c.cookie)
	}
	if method != "GET" {
		r.Header.Set("Origin", c.origin)
		r.Header.Set("X-CSRF-Token", c.csrf)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	c.h.ServeHTTP(w, r)
	return w
}
func (c *client) login(t *testing.T) {
	t.Helper()
	b, _ := json.Marshal(map[string]string{"password": c.password})
	w := c.request("POST", "/api/login", string(b), nil)
	if w.Code != 200 {
		t.Fatalf("login status %d", w.Code)
	}
	cookies := w.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatal("missing cookie")
	}
	c.cookie = cookies[0]
	var data struct {
		CSRF string `json:"csrfToken"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
		t.Fatal(err)
	}
	c.csrf = data.CSRF
	if c.csrf == "" {
		t.Fatal("missing csrf")
	}
}
func TestLogin(t *testing.T) {
	for _, https := range []bool{false, true} {
		c, _ := setup(t, https)
		if w := c.request("POST", "/api/login", `{"password":"wrong"}`, nil); w.Code != 401 {
			t.Fatal("wrong password", w.Code)
		}
		c.login(t)
		if !c.cookie.HttpOnly || c.cookie.SameSite != http.SameSiteStrictMode || c.cookie.Secure != https || c.cookie.Path != "/" {
			t.Fatal("unsafe cookie")
		}
		w := c.request("GET", "/api/session", "", nil)
		if w.Code != 200 || strings.Contains(w.Body.String(), "hash") || strings.Contains(w.Body.String(), c.password) {
			t.Fatal("unsafe session response", w.Code)
		}
		if w = c.request("POST", "/api/logout", "", nil); w.Code != 204 {
			t.Fatal("logout", w.Code)
		}
		if w = c.request("GET", "/api/session", "", nil); w.Code != 401 {
			t.Fatal("logout ineffective")
		}
	}
}
func TestAuthBoundary(t *testing.T) {
	c, s := setup(t, false)
	for _, origin := range []string{"", "http://evil.test", "null"} {
		if w := c.request("POST", "/api/login", `{"password":"wrong"}`, map[string]string{"Origin": origin}); w.Code != 403 {
			t.Fatal("origin", w.Code)
		}
	}
	c.login(t)
	for _, headers := range []map[string]string{{"X-CSRF-Token": ""}, {"X-CSRF-Token": "wrong"}, {"Origin": "http://evil.test"}} {
		if w := c.request("POST", "/api/tokens", `{"name":"ci","grants":[{"repository":"*","permission":"read"}]}`, headers); w.Code != 403 {
			t.Fatal("csrf/origin", w.Code)
		}
	}
	for _, body := range []string{`{"password":"wrong","unexpected":true}`, `{"password":"wrong"} {}`, `{"password":`} {
		if w := c.request("POST", "/api/login", body, nil); w.Code != 400 {
			t.Fatal("strict JSON", w.Code)
		}
	}
	if w := c.request("POST", "/api/login", `{"password":"`+strings.Repeat("x", 70*1024)+`"}`, nil); w.Code != 413 {
		t.Fatal("body limit", w.Code)
	}
	_, token, err := s.CreateToken("docker", []auth.Grant{{Repository: "*", Permission: "write"}})
	if err != nil {
		t.Fatal(err)
	}
	c.cookie = nil
	if w := c.request("GET", "/api/tokens", "", map[string]string{"Authorization": "Bearer " + token}); w.Code != 401 {
		t.Fatal("docker credential reached admin")
	}
}
func TestTokenAPI(t *testing.T) {
	c, _ := setup(t, false)
	c.login(t)
	w := c.request("POST", "/api/tokens", `{"name":"ci","grants":[{"repository":"team/app","permission":"write"}]}`, nil)
	if w.Code != 201 {
		t.Fatal("create", w.Code)
	}
	var created struct {
		Token  auth.TokenRecord `json:"token"`
		Secret string           `json:"secret"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Secret == "" {
		t.Fatal("missing secret")
	}
	w = c.request("GET", "/api/tokens", "", nil)
	if w.Code != 200 || strings.Contains(w.Body.String(), created.Secret) || strings.Contains(w.Body.String(), "hash") {
		t.Fatal("unsafe token list")
	}
	w = c.request("DELETE", "/api/tokens/"+created.Token.ID, "", nil)
	if w.Code != 204 {
		t.Fatal("revoke", w.Code)
	}
	if w = c.request("DELETE", "/api/tokens/"+created.Token.ID, "", nil); w.Code != 404 {
		t.Fatal("missing token", w.Code)
	}
}
func TestSecurityHeadersAndRoot(t *testing.T) {
	c, _ := setup(t, false)
	for _, path := range []string{"/", "/app.js", "/style.css", "/healthz"} {
		w := c.request("GET", path, "", nil)
		if w.Code != 200 {
			t.Fatal(path, w.Code)
		}
		if strings.Contains(w.Body.String(), c.password) || w.Header().Get("X-Content-Type-Options") != "nosniff" || w.Header().Get("Content-Security-Policy") == "" {
			t.Fatal("unsafe shell")
		}
	}
	if w := c.request("GET", "/unknown", "", nil); w.Code != 404 {
		t.Fatal("unknown route", w.Code)
	}
	if w := c.request("GET", "/api/session", "", nil); w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("API cached")
	}
}
func TestLoginRateLimit(t *testing.T) {
	c, _ := setup(t, false)
	for i := 0; i < 6; i++ {
		w := c.request("POST", "/api/login", `{"password":"wrong"}`, map[string]string{"X-Forwarded-For": time.Now().String()})
		want := 401
		if i == 5 {
			want = 429
		}
		if w.Code != want {
			t.Fatalf("attempt %d status=%d want=%d", i+1, w.Code, want)
		}
		if i == 5 && w.Header().Get("Retry-After") == "" {
			t.Fatal("missing retry")
		}
	}
}

func TestLimiterExpiryGlobalAndBound(t *testing.T) {
	now := time.Now()
	var l limiter
	for i := 0; i < 5; i++ {
		if l.allow("ip", now) != 0 {
			t.Fatal("early rate limit")
		}
	}
	if l.allow("ip", now) == 0 {
		t.Fatal("missing IP limit")
	}
	if l.allow("ip", now.Add(time.Minute)) != 0 {
		t.Fatal("window did not reset")
	}
	l = limiter{}
	for i := 0; i < 60; i++ {
		if l.allow(strconv.Itoa(i), now) != 0 {
			t.Fatal("early global limit")
		}
	}
	if l.allow("another", now) == 0 {
		t.Fatal("missing global limit")
	}
	l = limiter{ips: map[string]window{}}
	for i := 0; i < 4096; i++ {
		l.ips[strconv.Itoa(i)] = window{start: now, count: 1}
	}
	if l.allow("new", now) == 0 || len(l.ips) != 4096 {
		t.Fatal("map not bounded")
	}
	if l.allow("new", now.Add(time.Minute)) != 0 || len(l.ips) != 1 {
		t.Fatal("expired map not swept")
	}
}

func TestAdminSessionCannotAuthenticateRegistry(t *testing.T) {
	c, s := setup(t, false)
	c.login(t)
	if w := c.request("GET", "/v2/", "", nil); w.Code != 401 {
		t.Fatal("UI session authenticated registry", w.Code)
	}
	_, token, e := s.CreateToken("ci", []auth.Grant{{Repository: "*", Permission: "read"}})
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(map[string]string{"password": token})
	if w := c.request("POST", "/api/login", string(b), nil); w.Code != 401 {
		t.Fatal("Docker token authenticated UI", w.Code)
	}
}
