package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

type testRefreshStore struct {
	mu      sync.Mutex
	loadVal string
	saved   []string
	loadErr error
	saveErr error
}

func (s *testRefreshStore) Load(_ context.Context) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadVal, s.loadErr
}

func (s *testRefreshStore) Save(_ context.Context, refreshToken string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.saveErr != nil {
		return s.saveErr
	}
	s.saved = append(s.saved, refreshToken)
	s.loadVal = refreshToken
	return nil
}

func TestSpotifyAuthorizeURL(t *testing.T) {
	t.Parallel()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", nil)
	link, err := client.AuthorizeURL("https://example.com/cb", "scope1 scope2", "abc")
	if err != nil {
		t.Fatalf("AuthorizeURL returned error: %v", err)
	}

	u, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	q := u.Query()
	if q.Get("client_id") != "id" || q.Get("state") != "abc" {
		t.Fatalf("unexpected authorize query: %s", u.RawQuery)
	}
	if q.Get("redirect_uri") != "https://example.com/cb" {
		t.Fatalf("unexpected redirect_uri: %s", q.Get("redirect_uri"))
	}
}

func TestSpotifyExchangeAuthorizationCodeAndVerify(t *testing.T) {
	t.Parallel()

	store := &testRefreshStore{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/token" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		body, _ := url.ParseQuery(readErrorBody(r.Body))
		grantType := body.Get("grant_type")

		authHeader := r.Header.Get("Authorization")
		wantPrefix := "Basic " + base64.StdEncoding.EncodeToString([]byte("id:secret"))
		if authHeader != wantPrefix {
			t.Fatalf("unexpected auth header: %q", authHeader)
		}

		switch grantType {
		case "authorization_code":
			_, _ = w.Write([]byte(`{"access_token":"acc-code","expires_in":3600,"refresh_token":"refresh-from-code"}`))
		case "refresh_token":
			if body.Get("refresh_token") != "refresh-from-code" {
				t.Fatalf("unexpected refresh token: %s", body.Get("refresh_token"))
			}
			_, _ = w.Write([]byte(`{"access_token":"acc-verified","expires_in":3600}`))
		default:
			t.Fatalf("unexpected grant_type: %s", grantType)
		}
	}))
	defer ts.Close()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", store)
	client.accountsURL = ts.URL + "/api/token"

	refreshToken, err := client.ExchangeAuthorizationCode(context.Background(), "code123", "https://example.com/cb")
	if err != nil {
		t.Fatalf("ExchangeAuthorizationCode returned error: %v", err)
	}
	if refreshToken != "refresh-from-code" {
		t.Fatalf("unexpected refresh token: %q", refreshToken)
	}

	got, err := client.VerifyAndActivateRefreshToken(context.Background(), refreshToken)
	if err != nil {
		t.Fatalf("VerifyAndActivateRefreshToken returned error: %v", err)
	}
	if got != "refresh-from-code" {
		t.Fatalf("unexpected returned refresh token: %q", got)
	}

	if len(store.saved) != 1 || store.saved[0] != "refresh-from-code" {
		t.Fatalf("expected refresh token persisted once, got %+v", store.saved)
	}
}

func TestSpotifyCurrentStatusFallbackRecently(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token":
			_, _ = w.Write([]byte(`{"access_token":"acc","expires_in":3600,"refresh_token":"rt1"}`))
		case "/v1/me/player/currently-playing":
			w.WriteHeader(http.StatusNoContent)
		case "/v1/me/player/recently-played":
			_, _ = w.Write([]byte(`{"items":[{"played_at":"2026-01-01T00:00:00Z","track":{"name":"Last Song","artists":[{"name":"A"}],"album":{"images":[{"url":"img"}]},"external_urls":{"spotify":"track-url"}}}]}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt1", nil)
	client.apiBaseURL = ts.URL + "/v1"
	client.accountsURL = ts.URL + "/api/token"

	status, err := client.CurrentStatus(context.Background())
	if err != nil {
		t.Fatalf("CurrentStatus returned error: %v", err)
	}
	if status.IsPlaying || !status.FromRecent {
		t.Fatalf("expected recent fallback status, got %+v", status)
	}
	if status.TrackName != "Last Song" || status.Artists[0] != "A" {
		t.Fatalf("unexpected status payload: %+v", status)
	}
}

func TestSpotifyCurrentStatusRetryAfterUnauthorized(t *testing.T) {
	t.Parallel()

	currentCalls := 0
	tokenCalls := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token":
			tokenCalls++
			if tokenCalls == 1 {
				_, _ = w.Write([]byte(`{"access_token":"acc-old","expires_in":3600,"refresh_token":"rt1"}`))
				return
			}
			_, _ = w.Write([]byte(`{"access_token":"acc-new","expires_in":3600,"refresh_token":"rt1"}`))
		case "/v1/me/player/currently-playing":
			currentCalls++
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if currentCalls == 1 {
				if token != "acc-old" {
					t.Fatalf("unexpected first token: %s", token)
				}
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if token != "acc-new" {
				t.Fatalf("unexpected second token: %s", token)
			}
			_, _ = w.Write([]byte(`{"is_playing":true,"progress_ms":123,"item":{"name":"Now","artists":[{"name":"Artist"}],"album":{"images":[{"url":"img"}]},"external_urls":{"spotify":"url"}}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt1", nil)
	client.apiBaseURL = ts.URL + "/v1"
	client.accountsURL = ts.URL + "/api/token"

	status, err := client.CurrentStatus(context.Background())
	if err != nil {
		t.Fatalf("CurrentStatus returned error: %v", err)
	}
	if !status.IsPlaying || status.TrackName != "Now" {
		t.Fatalf("unexpected status: %+v", status)
	}
	if tokenCalls != 2 || currentCalls != 2 {
		t.Fatalf("expected retry flow; tokenCalls=%d currentCalls=%d", tokenCalls, currentCalls)
	}
}

func TestSpotifySavedTracksCount(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token":
			_, _ = w.Write([]byte(`{"access_token":"acc","expires_in":3600,"refresh_token":"rt1"}`))
		case "/v1/me/tracks":
			_, _ = w.Write([]byte(`{"total":88}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt1", nil)
	client.apiBaseURL = ts.URL + "/v1"
	client.accountsURL = ts.URL + "/api/token"

	count, err := client.SavedTracksCount(context.Background())
	if err != nil {
		t.Fatalf("SavedTracksCount returned error: %v", err)
	}
	if count != 88 {
		t.Fatalf("unexpected count: %d", count)
	}
}

func TestSpotifyHasRefreshTokenLoadsFromStore(t *testing.T) {
	t.Parallel()

	store := &testRefreshStore{loadVal: "stored-token"}
	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", store)

	ok, err := client.HasRefreshToken(context.Background())
	if err != nil {
		t.Fatalf("HasRefreshToken returned error: %v", err)
	}
	if !ok {
		t.Fatalf("expected refresh token to be loaded")
	}

	client.mu.Lock()
	defer client.mu.Unlock()
	if client.refreshToken != "stored-token" {
		t.Fatalf("unexpected loaded refresh token: %q", client.refreshToken)
	}
}

func TestSpotifyApplyTokenPayloadLocked(t *testing.T) {
	t.Parallel()

	store := &testRefreshStore{}
	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt-old", store)

	client.mu.Lock()
	defer client.mu.Unlock()

	payload := spotifyTokenPayload{
		AccessToken:  "acc",
		ExpiresIn:    10,
		RefreshToken: "rt-new",
	}
	rt, err := client.applyTokenPayloadLocked(context.Background(), payload, "rt-old")
	if err != nil {
		t.Fatalf("applyTokenPayloadLocked returned error: %v", err)
	}
	if rt != "rt-new" || client.refreshToken != "rt-new" || client.accessToken != "acc" {
		t.Fatalf("unexpected token state: rt=%q refresh=%q access=%q", rt, client.refreshToken, client.accessToken)
	}
	if len(store.saved) != 1 || store.saved[0] != "rt-new" {
		t.Fatalf("expected new refresh token persisted, got %+v", store.saved)
	}
}

func TestSpotifyBuildStatusHelpers(t *testing.T) {
	t.Parallel()

	artists := []struct {
		Name string `json:"name"`
	}{{Name: " A "}, {Name: ""}, {Name: "B"}}

	names := artistNames(artists)
	if len(names) != 2 || names[0] != "A" || names[1] != "B" {
		t.Fatalf("unexpected artist names: %+v", names)
	}

	images := []struct {
		URL string `json:"url"`
	}{{URL: " img "}}
	if firstImageURL(images) != "img" {
		t.Fatalf("unexpected image url")
	}

	status := buildSpotifyStatus(true, " Song ", names, " img ", 1, " u ", " p ", true)
	if status.TrackName != "Song" || status.AlbumImage != "img" || status.TrackURL != "u" || status.PlayedAt != "p" {
		t.Fatalf("unexpected normalized status: %+v", status)
	}
}

func TestSpotifyRequestTokenDecodeError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer ts.Close()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt", nil)
	client.accountsURL = ts.URL

	_, err := client.requestToken(context.Background(), url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"rt"}})
	if err == nil || !strings.Contains(err.Error(), "decode spotify token response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}

func TestSpotifyCurrentStatusDecodeError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "acc",
				"expires_in":    3600,
				"refresh_token": "rt",
			})
		case "/v1/me/player/currently-playing":
			_, _ = w.Write([]byte(`{bad}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt", nil)
	client.apiBaseURL = ts.URL + "/v1"
	client.accountsURL = ts.URL + "/api/token"

	_, err := client.CurrentStatus(context.Background())
	if err == nil || !strings.Contains(err.Error(), "decode spotify currently-playing response") {
		t.Fatalf("expected decode error, got %v", err)
	}
}
