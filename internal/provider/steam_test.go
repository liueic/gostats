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

func TestSteamOwnedGamesSummaryWithSteamID(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/IPlayerService/GetOwnedGames/v0001/") {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		q := r.URL.Query()
		if q.Get("steamid") != "76561198000000000" {
			t.Fatalf("unexpected steamid: %s", q.Get("steamid"))
		}
		_, _ = w.Write([]byte(`{"response":{"game_count":2,"games":[{"playtime_forever":30,"playtime_2weeks":10},{"playtime_forever":90,"playtime_2weeks":20}]}}`))
	}))
	defer ts.Close()

	client := NewSteamClient(&http.Client{Timeout: 2 * time.Second}, "k")
	client.baseURL = ts.URL

	summary, err := client.OwnedGamesSummary(context.Background(), "76561198000000000")
	if err != nil {
		t.Fatalf("OwnedGamesSummary returned error: %v", err)
	}
	if summary.GameCount != 2 || summary.TotalMinutes != 120 || summary.RecentMinutes != 30 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
}

func TestSteamOwnedGamesSummaryWithVanity(t *testing.T) {
	t.Parallel()

	step := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		step++
		switch {
		case strings.HasPrefix(r.URL.Path, "/ISteamUser/ResolveVanityURL/v0001/"):
			_, _ = w.Write([]byte(`{"response":{"steamid":"76561198000000000","success":1}}`))
		case strings.HasPrefix(r.URL.Path, "/IPlayerService/GetOwnedGames/v0001/"):
			q := r.URL.Query()
			if q.Get("steamid") != "76561198000000000" {
				t.Fatalf("unexpected steamid after resolve: %s", q.Get("steamid"))
			}
			_, _ = w.Write([]byte(`{"response":{"game_count":1,"games":[{"playtime_forever":5,"playtime_2weeks":2}]}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer ts.Close()

	client := NewSteamClient(&http.Client{Timeout: 2 * time.Second}, "k")
	client.baseURL = ts.URL

	summary, err := client.OwnedGamesSummary(context.Background(), "myvanity")
	if err != nil {
		t.Fatalf("OwnedGamesSummary returned error: %v", err)
	}
	if summary.SteamID != "76561198000000000" || summary.GameCount != 1 || summary.TotalMinutes != 5 || summary.RecentMinutes != 2 {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if step != 2 {
		t.Fatalf("expected 2 upstream calls, got %d", step)
	}
}

func TestSteamOwnedGamesSummaryValidation(t *testing.T) {
	t.Parallel()

	client := NewSteamClient(&http.Client{Timeout: 2 * time.Second}, "")
	if _, err := client.OwnedGamesSummary(context.Background(), "123"); err == nil || !strings.Contains(err.Error(), "STEAM_API_KEY") {
		t.Fatalf("expected missing key error, got %v", err)
	}

	client = NewSteamClient(&http.Client{Timeout: 2 * time.Second}, "k")
	if _, err := client.OwnedGamesSummary(context.Background(), " "); err == nil || !strings.Contains(err.Error(), "steam id is empty") {
		t.Fatalf("expected empty id error, got %v", err)
	}
}

func TestSteamResolveVanityFailure(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/ISteamUser/ResolveVanityURL/v0001/") {
			_, _ = w.Write([]byte(`{"response":{"success":42,"message":"No match"}}`))
			return
		}
		t.Fatalf("unexpected path: %s", r.URL.Path)
	}))
	defer ts.Close()

	client := NewSteamClient(&http.Client{Timeout: 2 * time.Second}, "k")
	client.baseURL = ts.URL

	_, err := client.OwnedGamesSummary(context.Background(), "vanity")
	if err == nil || !strings.Contains(err.Error(), "No match") {
		t.Fatalf("expected vanity resolve error, got %v", err)
	}
}

func TestLooksLikeSteamID64(t *testing.T) {
	t.Parallel()

	if !looksLikeSteamID64("76561198000000000") {
		t.Fatalf("expected valid steam id")
	}
	if looksLikeSteamID64("12345") {
		t.Fatalf("expected invalid short steam id")
	}
	if looksLikeSteamID64("7656119800000000a") {
		t.Fatalf("expected invalid non-digit steam id")
	}
}

func TestSteamRequestIncludesAPIKey(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q, _ := url.ParseQuery(r.URL.RawQuery)
		if q.Get("key") != "api-key-1" {
			t.Fatalf("unexpected key: %q", q.Get("key"))
		}
		_, _ = w.Write([]byte(`{"response":{"game_count":0,"games":[]}}`))
	}))
	defer ts.Close()

	client := NewSteamClient(&http.Client{Timeout: 2 * time.Second}, "api-key-1")
	client.baseURL = ts.URL

	if _, err := client.OwnedGamesSummary(context.Background(), "76561198000000000"); err != nil {
		t.Fatalf("OwnedGamesSummary returned error: %v", err)
	}
}
