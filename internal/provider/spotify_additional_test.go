package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSpotifyAuthorizeAndOAuthValidation(t *testing.T) {
	t.Parallel()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "", "secret", "", nil)
	if _, err := client.AuthorizeURL("https://example.com/callback", "scope", "state"); err == nil || !strings.Contains(err.Error(), "SPOTIFY_CLIENT_ID") {
		t.Fatalf("expected missing oauth config error, got %v", err)
	}

	client = NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", nil)
	if _, err := client.AuthorizeURL("", "scope", "state"); err == nil || !strings.Contains(err.Error(), "redirect uri is empty") {
		t.Fatalf("expected empty redirect error, got %v", err)
	}
	if _, err := client.AuthorizeURL("https://example.com/callback", "", "state"); err == nil || !strings.Contains(err.Error(), "scopes are empty") {
		t.Fatalf("expected empty scopes error, got %v", err)
	}
}

func TestSpotifyExchangeAndVerifyValidation(t *testing.T) {
	t.Parallel()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "", "secret", "", nil)
	if _, err := client.ExchangeAuthorizationCode(context.Background(), "code", "https://example.com/callback"); err == nil || !strings.Contains(err.Error(), "SPOTIFY_CLIENT_ID") {
		t.Fatalf("expected missing oauth config error, got %v", err)
	}

	client = NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", nil)
	if _, err := client.ExchangeAuthorizationCode(context.Background(), "", "https://example.com/callback"); err == nil || !strings.Contains(err.Error(), "oauth code is empty") {
		t.Fatalf("expected empty code error, got %v", err)
	}
	if _, err := client.ExchangeAuthorizationCode(context.Background(), "code", ""); err == nil || !strings.Contains(err.Error(), "redirect uri is empty") {
		t.Fatalf("expected empty redirect error, got %v", err)
	}

	if _, err := client.VerifyAndActivateRefreshToken(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "refresh token is empty") {
		t.Fatalf("expected empty refresh token error, got %v", err)
	}
}

func TestSpotifyHasRefreshTokenEmptyAndError(t *testing.T) {
	t.Parallel()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", &testRefreshStore{loadVal: "  "})
	ok, err := client.HasRefreshToken(context.Background())
	if err != nil {
		t.Fatalf("HasRefreshToken should not fail for empty stored token: %v", err)
	}
	if ok {
		t.Fatalf("expected has refresh token=false for empty stored token")
	}

	client = NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", &testRefreshStore{loadErr: context.DeadlineExceeded})
	if _, err := client.HasRefreshToken(context.Background()); err == nil || !strings.Contains(err.Error(), "load refresh token from store") {
		t.Fatalf("expected load error, got %v", err)
	}
}

func TestSpotifyAccessTokenForRequestInitFlows(t *testing.T) {
	t.Parallel()

	t.Run("load token from store", func(t *testing.T) {
		t.Parallel()

		store := &testRefreshStore{loadVal: "stored-refresh"}
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/token" {
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("refresh_token"); got != "stored-refresh" {
				t.Fatalf("unexpected refresh token in request: %q", got)
			}
			_, _ = w.Write([]byte(`{"access_token":"acc","expires_in":3600}`))
		}))
		defer ts.Close()

		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", store)
		client.accountsURL = ts.URL + "/api/token"

		token, err := client.accessTokenForRequest(context.Background(), false)
		if err != nil {
			t.Fatalf("accessTokenForRequest returned error: %v", err)
		}
		if token != "acc" {
			t.Fatalf("unexpected access token: %q", token)
		}
		if len(store.saved) != 0 {
			t.Fatalf("store should not persist when loaded token already exists, got %+v", store.saved)
		}
	})

	t.Run("seed store with env refresh token", func(t *testing.T) {
		t.Parallel()

		store := &testRefreshStore{loadVal: ""}
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := r.ParseForm(); err != nil {
				t.Fatalf("parse form: %v", err)
			}
			if got := r.Form.Get("refresh_token"); got != "env-refresh" {
				t.Fatalf("unexpected refresh token in request: %q", got)
			}
			_, _ = w.Write([]byte(`{"access_token":"acc2","expires_in":3600}`))
		}))
		defer ts.Close()

		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "env-refresh", store)
		client.accountsURL = ts.URL

		token, err := client.accessTokenForRequest(context.Background(), false)
		if err != nil {
			t.Fatalf("accessTokenForRequest returned error: %v", err)
		}
		if token != "acc2" {
			t.Fatalf("unexpected access token: %q", token)
		}
		if len(store.saved) != 1 || store.saved[0] != "env-refresh" {
			t.Fatalf("expected seed save to store, got %+v", store.saved)
		}
	})

	t.Run("store load error", func(t *testing.T) {
		t.Parallel()

		store := &testRefreshStore{loadErr: context.Canceled}
		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", store)
		if _, err := client.accessTokenForRequest(context.Background(), false); err == nil || !strings.Contains(err.Error(), "load refresh token from store") {
			t.Fatalf("expected store load error, got %v", err)
		}
	})

	t.Run("no refresh token anywhere", func(t *testing.T) {
		t.Parallel()

		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", &testRefreshStore{})
		if _, err := client.accessTokenForRequest(context.Background(), false); err == nil || !strings.Contains(err.Error(), "store is empty") {
			t.Fatalf("expected missing refresh token error, got %v", err)
		}
	})
}

func TestSpotifyValidateConfigErrors(t *testing.T) {
	t.Parallel()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "", "secret", "rt", nil)
	if _, err := client.CurrentStatus(context.Background()); err == nil || !strings.Contains(err.Error(), "SPOTIFY_CLIENT_ID") {
		t.Fatalf("expected missing client id error, got %v", err)
	}

	client = NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "", "rt", nil)
	if _, err := client.CurrentStatus(context.Background()); err == nil || !strings.Contains(err.Error(), "SPOTIFY_CLIENT_SECRET") {
		t.Fatalf("expected missing client secret error, got %v", err)
	}

	client = NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "", nil)
	if _, err := client.SavedTracksCount(context.Background()); err == nil || !strings.Contains(err.Error(), "SPOTIFY_REFRESH_TOKEN") {
		t.Fatalf("expected missing refresh token error, got %v", err)
	}
}

func TestSpotifyCurrentAndSavedTracksErrorBranches(t *testing.T) {
	t.Parallel()

	t.Run("currently playing non-200", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/token":
				_, _ = w.Write([]byte(`{"access_token":"acc","expires_in":3600,"refresh_token":"rt"}`))
			case "/v1/me/player/currently-playing":
				http.Error(w, "upstream bad", http.StatusInternalServerError)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer ts.Close()

		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt", nil)
		client.apiBaseURL = ts.URL + "/v1"
		client.accountsURL = ts.URL + "/api/token"

		_, err := client.CurrentStatus(context.Background())
		if err == nil || !strings.Contains(err.Error(), "currently-playing api status 500") {
			t.Fatalf("expected currently-playing status error, got %v", err)
		}
	})

	t.Run("saved tracks non-200", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/token":
				_, _ = w.Write([]byte(`{"access_token":"acc","expires_in":3600,"refresh_token":"rt"}`))
			case "/v1/me/tracks":
				http.Error(w, "denied", http.StatusForbidden)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer ts.Close()

		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt", nil)
		client.apiBaseURL = ts.URL + "/v1"
		client.accountsURL = ts.URL + "/api/token"

		_, err := client.SavedTracksCount(context.Background())
		if err == nil || !strings.Contains(err.Error(), "saved tracks api status 403") {
			t.Fatalf("expected saved tracks status error, got %v", err)
		}
	})

	t.Run("saved tracks decode error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/token":
				_, _ = w.Write([]byte(`{"access_token":"acc","expires_in":3600,"refresh_token":"rt"}`))
			case "/v1/me/tracks":
				_, _ = w.Write([]byte(`bad`))
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer ts.Close()

		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt", nil)
		client.apiBaseURL = ts.URL + "/v1"
		client.accountsURL = ts.URL + "/api/token"

		_, err := client.SavedTracksCount(context.Background())
		if err == nil || !strings.Contains(err.Error(), "decode spotify saved tracks response") {
			t.Fatalf("expected saved tracks decode error, got %v", err)
		}
	})
}

func TestSpotifyRecentlyPlayedFallbackBranches(t *testing.T) {
	t.Parallel()

	t.Run("fallback api status error", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/token":
				_, _ = w.Write([]byte(`{"access_token":"acc","expires_in":3600,"refresh_token":"rt"}`))
			case "/v1/me/player/currently-playing":
				w.WriteHeader(http.StatusNoContent)
			case "/v1/me/player/recently-played":
				http.Error(w, "boom", http.StatusBadGateway)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer ts.Close()

		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt", nil)
		client.apiBaseURL = ts.URL + "/v1"
		client.accountsURL = ts.URL + "/api/token"

		_, err := client.CurrentStatus(context.Background())
		if err == nil || !strings.Contains(err.Error(), "recently-played api status 502") {
			t.Fatalf("expected recently-played status error, got %v", err)
		}
	})

	t.Run("fallback empty items", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/token":
				_, _ = w.Write([]byte(`{"access_token":"acc","expires_in":3600,"refresh_token":"rt"}`))
			case "/v1/me/player/currently-playing":
				w.WriteHeader(http.StatusNoContent)
			case "/v1/me/player/recently-played":
				_, _ = w.Write([]byte(`{"items":[]}`))
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer ts.Close()

		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt", nil)
		client.apiBaseURL = ts.URL + "/v1"
		client.accountsURL = ts.URL + "/api/token"

		_, err := client.CurrentStatus(context.Background())
		if err == nil || !strings.Contains(err.Error(), "returned empty items") {
			t.Fatalf("expected empty items error, got %v", err)
		}
	})

	t.Run("currently-playing item nil triggers fallback", func(t *testing.T) {
		t.Parallel()

		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/token":
				_, _ = w.Write([]byte(`{"access_token":"acc","expires_in":3600,"refresh_token":"rt"}`))
			case "/v1/me/player/currently-playing":
				_, _ = w.Write([]byte(`{"is_playing":false,"item":null}`))
			case "/v1/me/player/recently-played":
				_, _ = w.Write([]byte(`{"items":[{"played_at":"2026-01-01T00:00:00Z","track":{"name":"Last","artists":[{"name":"A"}],"album":{"images":[{"url":"img"}]},"external_urls":{"spotify":"url"}}}]}`))
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer ts.Close()

		client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt", nil)
		client.apiBaseURL = ts.URL + "/v1"
		client.accountsURL = ts.URL + "/api/token"

		status, err := client.CurrentStatus(context.Background())
		if err != nil {
			t.Fatalf("CurrentStatus returned error: %v", err)
		}
		if !status.FromRecent || status.TrackName != "Last" {
			t.Fatalf("expected fallback status from recent track, got %+v", status)
		}
	})
}

func TestSpotifyRequestTokenNon200Error(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "invalid_grant", http.StatusUnauthorized)
	}))
	defer ts.Close()

	client := NewSpotifyClient(&http.Client{Timeout: 2 * time.Second}, "id", "secret", "rt", nil)
	client.accountsURL = ts.URL

	_, err := client.requestToken(context.Background(), url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {"rt"},
	})
	if err == nil || !strings.Contains(err.Error(), "spotify token api status 401") {
		t.Fatalf("expected non-200 status error, got %v", err)
	}
}
