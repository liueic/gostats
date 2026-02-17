package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gostats/internal/provider"
)

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("X-Original-Host", req.URL.Host)
	clone.URL.Scheme = t.target.Scheme
	clone.URL.Host = t.target.Host
	clone.Host = t.target.Host
	return t.base.RoundTrip(clone)
}

type fakeUpstream struct {
	mu sync.Mutex

	githubCalls         int
	steamResolveCalls   int
	steamOwnedCalls     int
	spotifyTokenCalls   int
	spotifyCurrentCalls int
	spotifySavedCalls   int
	lastOAuthCode       string
}

func (f *fakeUpstream) handler(w http.ResponseWriter, r *http.Request) {
	host := r.Header.Get("X-Original-Host")
	switch host {
	case "api.github.com":
		f.handleGitHub(w, r)
	case "api.steampowered.com":
		f.handleSteam(w, r)
	case "accounts.spotify.com":
		f.handleSpotifyAccounts(w, r)
	case "api.spotify.com":
		f.handleSpotifyAPI(w, r)
	default:
		http.Error(w, "unknown host "+host, http.StatusBadRequest)
	}
}

func (f *fakeUpstream) handleGitHub(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, "/users/") {
		http.NotFound(w, r)
		return
	}
	f.mu.Lock()
	f.githubCalls++
	f.mu.Unlock()
	_, _ = w.Write([]byte(`{"followers":123}`))
}

func (f *fakeUpstream) handleSteam(w http.ResponseWriter, r *http.Request) {
	switch {
	case strings.HasPrefix(r.URL.Path, "/ISteamUser/ResolveVanityURL/v0001/"):
		f.mu.Lock()
		f.steamResolveCalls++
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"response":{"steamid":"76561198000000000","success":1}}`))
	case strings.HasPrefix(r.URL.Path, "/IPlayerService/GetOwnedGames/v0001/"):
		f.mu.Lock()
		f.steamOwnedCalls++
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"response":{"game_count":2,"games":[{"playtime_forever":60,"playtime_2weeks":15},{"playtime_forever":30,"playtime_2weeks":45}]}}`))
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeUpstream) handleSpotifyAccounts(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/token" {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()

	f.mu.Lock()
	f.spotifyTokenCalls++
	f.mu.Unlock()

	switch r.Form.Get("grant_type") {
	case "authorization_code":
		f.mu.Lock()
		f.lastOAuthCode = r.Form.Get("code")
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"access_token":"acc-code","expires_in":3600,"refresh_token":"from_code_refresh"}`))
	case "refresh_token":
		rt := r.Form.Get("refresh_token")
		switch rt {
		case "init_refresh":
			_, _ = w.Write([]byte(`{"access_token":"acc-init","expires_in":3600}`))
		case "from_code_refresh":
			_, _ = w.Write([]byte(`{"access_token":"acc-oauth","expires_in":3600}`))
		default:
			http.Error(w, "invalid refresh", http.StatusBadRequest)
		}
	default:
		http.Error(w, "invalid grant", http.StatusBadRequest)
	}
}

func (f *fakeUpstream) handleSpotifyAPI(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/v1/me/player/currently-playing":
		f.mu.Lock()
		f.spotifyCurrentCalls++
		f.mu.Unlock()
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "missing token", http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"is_playing":true,"progress_ms":10,"item":{"name":"Now Song","artists":[{"name":"Singer"}],"album":{"images":[{"url":"img"}]},"external_urls":{"spotify":"track-url"}}}`))
	case "/v1/me/tracks":
		f.mu.Lock()
		f.spotifySavedCalls++
		f.mu.Unlock()
		_, _ = w.Write([]byte(`{"total":77}`))
	default:
		http.NotFound(w, r)
	}
}

func newServiceWithFakeUpstream(t *testing.T, refreshToken string, store provider.RefreshTokenStore, opts Options) (*Server, *fakeUpstream) {
	t.Helper()

	fake := &fakeUpstream{}
	upstream := httptest.NewServer(http.HandlerFunc(fake.handler))
	t.Cleanup(upstream.Close)

	target, err := url.Parse(upstream.URL)
	if err != nil {
		t.Fatalf("parse upstream url: %v", err)
	}
	httpClient := &http.Client{
		Timeout: 3 * time.Second,
		Transport: &rewriteTransport{
			target: target,
			base:   http.DefaultTransport,
		},
	}

	github := provider.NewGitHubClient(httpClient, "gh")
	steam := provider.NewSteamClient(httpClient, "steam")
	spotify := provider.NewSpotifyClient(httpClient, "id", "secret", refreshToken, store)

	if opts.CacheTTL <= 0 {
		opts.CacheTTL = 5 * time.Minute
	}
	if strings.TrimSpace(opts.SpotifyRedirectURI) == "" {
		opts.SpotifyRedirectURI = "https://blog.example/spotify/auth/callback"
	}

	return New(github, steam, spotify, opts), fake
}

func TestServerStatsJSONIntegrationAndCache(t *testing.T) {
	t.Parallel()

	srv, fake := newServiceWithFakeUpstream(t, "init_refresh", nil, Options{
		CacheTTL:           2 * time.Minute,
		CORSAllowedOrigins: []string{"*"},
	})
	handler := srv.Handler()

	for i := 0; i < 2; i++ {
		req := httptest.NewRequest(http.MethodGet, "/stats.json?github=alice&steam=myvanity&spotify=me", nil)
		req.Header.Set("Origin", "https://blog.example")
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Fatalf("request %d returned status %d: %s", i+1, rr.Code, rr.Body.String())
		}
		if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Fatalf("missing CORS header, got %q", got)
		}

		var items []map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &items); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(items) != 6 {
			t.Fatalf("unexpected item count: %d", len(items))
		}
		for _, item := range items {
			failed, _ := item["failed"].(bool)
			if failed {
				t.Fatalf("unexpected failed item: %+v", item)
			}
		}
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.githubCalls != 1 {
		t.Fatalf("expected github cached once, got %d", fake.githubCalls)
	}
	if fake.steamResolveCalls != 1 || fake.steamOwnedCalls != 1 {
		t.Fatalf("expected steam cached once, resolve=%d owned=%d", fake.steamResolveCalls, fake.steamOwnedCalls)
	}
	if fake.spotifyTokenCalls != 1 || fake.spotifyCurrentCalls != 1 || fake.spotifySavedCalls != 1 {
		t.Fatalf("expected spotify cached once, token=%d current=%d saved=%d", fake.spotifyTokenCalls, fake.spotifyCurrentCalls, fake.spotifySavedCalls)
	}
}

func TestServerSpotifyOAuthFlowIntegration(t *testing.T) {
	t.Parallel()

	tokenFile := filepath.Join(t.TempDir(), "spotify_refresh_token")
	store := provider.NewFileRefreshTokenStore(tokenFile)

	srv, fake := newServiceWithFakeUpstream(t, "", store, Options{
		CacheTTL:           2 * time.Minute,
		SpotifyRedirectURI: "https://blog.example/spotify/auth/callback",
	})
	handler := srv.Handler()

	// status before authorization
	{
		req := httptest.NewRequest(http.MethodGet, "https://blog.example/spotify/auth/status", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status before auth returned %d", rr.Code)
		}
		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if payload["configured"] != true || payload["hasRefreshToken"] != false {
			t.Fatalf("unexpected pre-auth status: %+v", payload)
		}
	}

	// start flow
	startReq := httptest.NewRequest(http.MethodGet, "https://blog.example/spotify/auth/start?format=json", nil)
	startRR := httptest.NewRecorder()
	handler.ServeHTTP(startRR, startReq)
	if startRR.Code != http.StatusOK {
		t.Fatalf("auth start returned %d: %s", startRR.Code, startRR.Body.String())
	}
	startRes := startRR.Result()
	if len(startRes.Cookies()) == 0 {
		t.Fatalf("expected state cookie on auth start")
	}
	stateCookie := startRes.Cookies()[0]

	var startPayload map[string]any
	if err := json.Unmarshal(startRR.Body.Bytes(), &startPayload); err != nil {
		t.Fatalf("decode auth start response: %v", err)
	}
	if startPayload["authorizeURL"] == "" {
		t.Fatalf("expected authorizeURL in start response")
	}

	// callback with state + code
	callbackReq := httptest.NewRequest(http.MethodGet, "https://blog.example/spotify/auth/callback?format=json&state="+url.QueryEscape(stateCookie.Value)+"&code=abc123", nil)
	callbackReq.AddCookie(stateCookie)
	callbackRR := httptest.NewRecorder()
	handler.ServeHTTP(callbackRR, callbackReq)
	if callbackRR.Code != http.StatusOK {
		t.Fatalf("auth callback returned %d: %s", callbackRR.Code, callbackRR.Body.String())
	}

	// status after authorization
	{
		req := httptest.NewRequest(http.MethodGet, "https://blog.example/spotify/auth/status", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("status after auth returned %d", rr.Code)
		}
		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode status: %v", err)
		}
		if payload["hasRefreshToken"] != true {
			t.Fatalf("expected hasRefreshToken=true, got %+v", payload)
		}
	}

	raw, err := os.ReadFile(tokenFile)
	if err != nil {
		t.Fatalf("read token file: %v", err)
	}
	if strings.TrimSpace(string(raw)) != "from_code_refresh" {
		t.Fatalf("unexpected token file content: %q", string(raw))
	}

	// spotify endpoint should now work
	{
		req := httptest.NewRequest(http.MethodGet, "/stats/spotifyplaying/me", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("spotifyplaying returned %d: %s", rr.Code, rr.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(rr.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode spotifyplaying: %v", err)
		}
		if failed, _ := payload["failed"].(bool); failed {
			t.Fatalf("expected spotifyplaying success, got %+v", payload)
		}
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.lastOAuthCode != "abc123" {
		t.Fatalf("unexpected oauth code sent upstream: %q", fake.lastOAuthCode)
	}
}

func TestServerSpotifyOAuthCallbackStateMismatch(t *testing.T) {
	t.Parallel()

	srv, _ := newServiceWithFakeUpstream(t, "", provider.NewFileRefreshTokenStore(filepath.Join(t.TempDir(), "rt")), Options{
		CacheTTL:           1 * time.Minute,
		SpotifyRedirectURI: "https://blog.example/spotify/auth/callback",
	})
	handler := srv.Handler()

	// Get a valid cookie first.
	startReq := httptest.NewRequest(http.MethodGet, "https://blog.example/spotify/auth/start?format=json", nil)
	startRR := httptest.NewRecorder()
	handler.ServeHTTP(startRR, startReq)
	if startRR.Code != http.StatusOK {
		t.Fatalf("auth start returned %d", startRR.Code)
	}
	stateCookie := startRR.Result().Cookies()[0]

	callbackReq := httptest.NewRequest(http.MethodGet, "https://blog.example/spotify/auth/callback?format=json&state=wrong&code=abc", nil)
	callbackReq.AddCookie(stateCookie)
	callbackRR := httptest.NewRecorder()
	handler.ServeHTTP(callbackRR, callbackReq)

	if callbackRR.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for state mismatch, got %d", callbackRR.Code)
	}
}

func TestServerUnsupportedSource(t *testing.T) {
	t.Parallel()

	srv, _ := newServiceWithFakeUpstream(t, "init_refresh", nil, Options{CacheTTL: 1 * time.Minute})
	req := httptest.NewRequest(http.MethodGet, "/stats/unknown/key", nil)
	rr := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rr, req)

	if rr.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unsupported source, got %d", rr.Code)
	}
}
