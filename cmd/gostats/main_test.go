package main

import (
	"reflect"
	"testing"
	"time"
)

func TestGetEnv(t *testing.T) {
	t.Setenv("GOSTATS_TEST_ENV", "value")
	if got := getEnv("GOSTATS_TEST_ENV", "fallback"); got != "value" {
		t.Fatalf("expected env value, got %q", got)
	}

	t.Setenv("GOSTATS_TEST_ENV", "")
	if got := getEnv("GOSTATS_TEST_ENV", "fallback"); got != "fallback" {
		t.Fatalf("expected fallback value, got %q", got)
	}
}

func TestParseDurationOrDefault(t *testing.T) {
	t.Parallel()

	if got := parseDurationOrDefault("30s", 5*time.Minute); got != 30*time.Second {
		t.Fatalf("unexpected parsed duration: %s", got)
	}

	if got := parseDurationOrDefault("bad", 5*time.Minute); got != 5*time.Minute {
		t.Fatalf("invalid duration should return fallback, got %s", got)
	}

	if got := parseDurationOrDefault("0s", 5*time.Minute); got != 5*time.Minute {
		t.Fatalf("non-positive duration should return fallback, got %s", got)
	}
}

func TestEffectiveSpotifyRefreshTokenFile(t *testing.T) {
	t.Parallel()

	if got := effectiveSpotifyRefreshTokenFile("id", "secret", "./custom/token"); got != "./custom/token" {
		t.Fatalf("expected configured path, got %q", got)
	}

	if got := effectiveSpotifyRefreshTokenFile("id", "secret", " "); got != "./data/spotify_refresh_token" {
		t.Fatalf("expected default spotify token file path, got %q", got)
	}

	if got := effectiveSpotifyRefreshTokenFile("", "secret", " "); got != "" {
		t.Fatalf("expected empty path when oauth is not configured, got %q", got)
	}
}

func TestParseBoolOrDefault(t *testing.T) {
	t.Parallel()

	if !parseBoolOrDefault("true", false) {
		t.Fatalf("expected true")
	}
	if parseBoolOrDefault("false", true) {
		t.Fatalf("expected false")
	}
	if parseBoolOrDefault("bad", true) != true {
		t.Fatalf("unexpected fallback handling for invalid bool")
	}
}

func TestParseCSV(t *testing.T) {
	t.Parallel()

	if got := parseCSV(" "); got != nil {
		t.Fatalf("expected nil for empty csv, got %#v", got)
	}

	want := []string{"https://a.example", "https://b.example"}
	got := parseCSV(" https://a.example, https://b.example,https://a.example ")
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected csv parse result: got=%#v want=%#v", got, want)
	}
}
