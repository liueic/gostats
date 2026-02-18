package server

import (
	"context"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"gostats/internal/provider"
)

type tokenStoreErr struct {
	err error
}

func (s tokenStoreErr) Load(context.Context) (string, error) {
	return "", s.err
}

func (s tokenStoreErr) Save(context.Context, string) error {
	return nil
}

type captureResponseWriter struct {
	header http.Header
	status []int
	body   strings.Builder
}

func (w *captureResponseWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}

func (w *captureResponseWriter) WriteHeader(statusCode int) {
	w.status = append(w.status, statusCode)
}

func (w *captureResponseWriter) Write(p []byte) (int, error) {
	if len(w.status) == 0 {
		w.status = append(w.status, http.StatusOK)
	}
	return w.body.Write(p)
}

func TestNewAppliesDefaults(t *testing.T) {
	t.Parallel()

	srv := New(nil, nil, nil, nil, Options{})
	if srv.cache == nil {
		t.Fatalf("cache should be initialized")
	}
	if srv.spotifyOAuthScopes != defaultSpotifyOAuthScopes {
		t.Fatalf("unexpected default scopes: %q", srv.spotifyOAuthScopes)
	}
}

func TestIndexReturnsHTMLPanelAndJSONFallback(t *testing.T) {
	t.Parallel()

	srv := New(nil, nil, nil, nil, Options{})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("index expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("index expected html content-type, got %q", got)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "gostats 控制面板") {
		t.Fatalf("index page missing title")
	}
	if !strings.Contains(body, openSourceRepoURL) {
		t.Fatalf("index page missing repo url")
	}
	if !strings.Contains(body, "/stats/steam2weekstime/") {
		t.Fatalf("index page missing steam2weekstime endpoint hint")
	}

	req = httptest.NewRequest(http.MethodGet, "/?format=json", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("index json expected 200, got %d", rr.Code)
	}
	if got := rr.Header().Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("index json expected application/json, got %q", got)
	}
}

func TestHandlerOptionsCORS(t *testing.T) {
	t.Parallel()

	srv := New(nil, nil, nil, nil, Options{CORSAllowedOrigins: []string{"https://blog.example"}})

	req := httptest.NewRequest(http.MethodOptions, "/stats/github/alice", nil)
	req.Header.Set("Origin", "https://blog.example")
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for OPTIONS, got %d", rr.Code)
	}
	if rr.Header().Get("Access-Control-Allow-Origin") != "https://blog.example" {
		t.Fatalf("unexpected cors header: %q", rr.Header().Get("Access-Control-Allow-Origin"))
	}

	req = httptest.NewRequest(http.MethodOptions, "/stats/github/alice", nil)
	req.Header.Set("Origin", "https://evil.example")
	rr = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected 403 for disallowed preflight origin, got %d", rr.Code)
	}
}

func TestIndexAndHealthMethodNotAllowed(t *testing.T) {
	t.Parallel()

	srv := New(nil, nil, nil, nil, Options{})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("index expected 405, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("health expected 405, got %d", rr.Code)
	}
}

func TestStatsBySourcePathAndMethodValidation(t *testing.T) {
	t.Parallel()

	srv := New(nil, nil, nil, nil, Options{})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/stats/github/alice", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for method, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/stats/github", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for missing key, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/stats/github/alice", nil)
	req.URL.Path = "/stats/github/%zz"
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid escape, got %d", rr.Code)
	}
}

func TestBatchStatsValidationAndErrorStats(t *testing.T) {
	t.Parallel()

	spotify := provider.NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "", "", "", nil)
	srv := New(
		provider.NewGitHubClient(&http.Client{Timeout: 2 * time.Second}, ""),
		provider.NewSteamClient(&http.Client{Timeout: 2 * time.Second}, ""),
		spotify,
		nil,
		Options{},
	)
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/stats.json", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for method, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/stats.json", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for empty query, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/stats.json?steam=vanity&spotify=me", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for error stats payload, got %d", rr.Code)
	}

	var items []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode batch stats: %v", err)
	}
	if len(items) != 5 {
		t.Fatalf("expected 5 stats items, got %d", len(items))
	}
	for _, item := range items {
		if failed, _ := item["failed"].(bool); !failed {
			t.Fatalf("expected failed=true for config errors, item=%+v", item)
		}
	}

	req = httptest.NewRequest(http.MethodGet, "/stats.json?bangumi=alice", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected 200 for bangumi error stats payload, got %d", rr.Code)
	}

	items = nil
	if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
		t.Fatalf("decode bangumi batch stats: %v", err)
	}
	if len(items) != 8 {
		t.Fatalf("expected 8 bangumi stats items, got %d", len(items))
	}
	for _, item := range items {
		if failed, _ := item["failed"].(bool); !failed {
			t.Fatalf("expected bangumi failed=true for unconfigured client, item=%+v", item)
		}
	}
}

func TestStatsBySourceErrorPayload(t *testing.T) {
	t.Parallel()

	spotify := provider.NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "", "", "", nil)
	srv := New(
		provider.NewGitHubClient(&http.Client{Timeout: 2 * time.Second}, ""),
		provider.NewSteamClient(&http.Client{Timeout: 2 * time.Second}, ""),
		spotify,
		nil,
		Options{},
	)
	handler := srv.Handler()

	for _, path := range []string{
		"/stats/github/%2520",
		"/stats/steamgames/%2520",
		"/stats/steam2weekstime/%2520",
		"/stats/spotifyplaying/me",
		"/stats/spotifysaved/me",
		"/stats/bangumianime/%2520",
		"/stats/bangumigame/%2520",
		"/stats/bangumianimewatching/%2520",
		"/stats/bangumianimewatched/%2520",
		"/stats/bangumianimewish/%2520",
		"/stats/bangumigameplaying/%2520",
		"/stats/bangumigameplayed/%2520",
		"/stats/bangumigamewish/%2520",
	} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("%s expected 200, got %d", path, rr.Code)
		}
		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode payload for %s: %v", path, err)
		}
		if failed, _ := payload["failed"].(bool); !failed {
			t.Fatalf("%s expected failed=true, payload=%+v", path, payload)
		}
	}
}

func TestSpotifyAuthStartAndCallbackValidation(t *testing.T) {
	t.Parallel()

	configuredSpotify := provider.NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", nil)
	srv := New(nil, nil, configuredSpotify, nil, Options{})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodPost, "/spotify/auth/start", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("auth start method expected 405, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "https://blog.example/spotify/auth/start", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("auth start redirect expected 302, got %d", rr.Code)
	}
	if location := rr.Header().Get("Location"); !strings.Contains(location, "accounts.spotify.com/authorize") {
		t.Fatalf("unexpected redirect location: %q", location)
	}
	cookies := rr.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].Secure {
		t.Fatalf("expected secure oauth state cookie")
	}

	req = httptest.NewRequest(http.MethodGet, "/spotify/auth/callback?format=json&error=access_denied", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("callback error query expected 400, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/spotify/auth/callback?format=json&state=x", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("callback missing code expected 400, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/spotify/auth/callback?format=json&code=x", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("callback missing state expected 400, got %d", rr.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/spotify/auth/callback?format=json&code=x&state=y", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("callback missing cookie expected 400, got %d", rr.Code)
	}
}

func TestSpotifyAuthStatusBranches(t *testing.T) {
	t.Parallel()

	srv := New(nil, nil, nil, nil, Options{})
	handler := srv.Handler()

	req := httptest.NewRequest(http.MethodGet, "/spotify/auth/status", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status with nil spotify expected 200, got %d", rr.Code)
	}

	errClient := provider.NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", tokenStoreErr{err: errors.New("boom")})
	srv = New(nil, nil, errClient, nil, Options{})
	handler = srv.Handler()
	req = httptest.NewRequest(http.MethodGet, "/spotify/auth/status", nil)
	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("status with store error expected 500, got %d", rr.Code)
	}
}

func TestSpotifyAuthCallbackHTMLSuccess(t *testing.T) {
	t.Parallel()

	tokenFile := filepath.Join(t.TempDir(), "spotify_refresh_token")
	store := provider.NewFileRefreshTokenStore(tokenFile)
	srv, _ := newServiceWithFakeUpstream(t, "", store, Options{
		CacheTTL:           1 * time.Minute,
		SpotifyRedirectURI: "https://blog.example/spotify/auth/callback",
	})
	handler := srv.Handler()

	startReq := httptest.NewRequest(http.MethodGet, "https://blog.example/spotify/auth/start?format=json", nil)
	startRR := httptest.NewRecorder()
	handler.ServeHTTP(startRR, startReq)
	if startRR.Code != http.StatusOK {
		t.Fatalf("auth start expected 200, got %d", startRR.Code)
	}
	stateCookie := startRR.Result().Cookies()[0]

	callbackReq := httptest.NewRequest(http.MethodGet, "https://blog.example/spotify/auth/callback?state="+stateCookie.Value+"&code=abc", nil)
	callbackReq.AddCookie(stateCookie)
	callbackRR := httptest.NewRecorder()
	handler.ServeHTTP(callbackRR, callbackReq)

	if callbackRR.Code != http.StatusOK {
		t.Fatalf("callback expected 200, got %d: %s", callbackRR.Code, callbackRR.Body.String())
	}
	if ctype := callbackRR.Header().Get("Content-Type"); !strings.HasPrefix(ctype, "text/html") {
		t.Fatalf("unexpected content type: %q", ctype)
	}
	if !strings.Contains(callbackRR.Body.String(), "Spotify authorization completed") {
		t.Fatalf("unexpected callback body: %q", callbackRR.Body.String())
	}
}

func TestUtilityFunctions(t *testing.T) {
	t.Parallel()

	state, err := randomState()
	if err != nil {
		t.Fatalf("randomState returned error: %v", err)
	}
	if len(state) != 32 {
		t.Fatalf("state length should be 32 hex chars, got %d", len(state))
	}
	if _, err := hex.DecodeString(state); err != nil {
		t.Fatalf("state is not valid hex: %v", err)
	}

	if !constantTimeStringEqual("abc", "abc") {
		t.Fatalf("equal strings should match")
	}
	if constantTimeStringEqual("ab", "abc") {
		t.Fatalf("different lengths should not match")
	}
	if constantTimeStringEqual("abc", "abd") {
		t.Fatalf("different content should not match")
	}

	srv := &Server{}

	r := httptest.NewRequest(http.MethodGet, "http://example.com/anything", nil)
	r.Header.Set("X-Forwarded-Proto", " https, http ")
	if got := srv.requestScheme(r); got != "http" {
		t.Fatalf("unexpected scheme when proxy headers are not trusted: %q", got)
	}

	r = httptest.NewRequest(http.MethodGet, "http://example.com/anything", nil)
	r.Header.Del("X-Forwarded-Proto")
	r.TLS = &tls.ConnectionState{}
	if got := srv.requestScheme(r); got != "https" {
		t.Fatalf("unexpected tls scheme: %q", got)
	}

	r = httptest.NewRequest(http.MethodGet, "http://blog.example/path", nil)
	if got := srv.effectiveSpotifyRedirectURI(r); got != "http://blog.example/spotify/auth/callback" {
		t.Fatalf("unexpected derived redirect uri: %q", got)
	}

	srv = &Server{trustProxyHeaders: true}
	r = httptest.NewRequest(http.MethodGet, "http://internal:8080/path", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-Forwarded-Host", "example.com")
	if got := srv.effectiveSpotifyRedirectURI(r); got != "https://example.com/spotify/auth/callback" {
		t.Fatalf("unexpected forwarded redirect uri: %q", got)
	}

	r = httptest.NewRequest(http.MethodGet, "http://internal:8080/path", nil)
	r.Header.Set("X-Forwarded-Host", " example.com, proxy.local ")
	if got := srv.requestHost(r); got != "example.com" {
		t.Fatalf("unexpected forwarded host: %q", got)
	}
}

func TestWriteJSONEncodeFailureFallback(t *testing.T) {
	t.Parallel()

	w := &captureResponseWriter{}
	writeJSON(w, http.StatusOK, map[string]any{
		"bad": make(chan int),
	})

	if len(w.status) < 2 {
		t.Fatalf("expected writeJSON fallback to call WriteHeader twice, got %+v", w.status)
	}
	if w.status[len(w.status)-1] != http.StatusInternalServerError {
		t.Fatalf("expected final status 500, got %+v", w.status)
	}
	if !strings.Contains(w.body.String(), "failed to write response") {
		t.Fatalf("unexpected fallback body: %q", w.body.String())
	}
}
