package provider

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestGitHubFollowersSuccess(t *testing.T) {
	t.Parallel()

	tokenSeen := ""
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users/alice" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		tokenSeen = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"followers":42}`))
	}))
	defer ts.Close()

	client := NewGitHubClient(&http.Client{Timeout: 2 * time.Second}, "my-token")
	client.baseURL = ts.URL

	count, err := client.Followers(context.Background(), "alice")
	if err != nil {
		t.Fatalf("Followers returned error: %v", err)
	}
	if count != 42 {
		t.Fatalf("unexpected followers: %d", count)
	}
	if tokenSeen != "Bearer my-token" {
		t.Fatalf("unexpected authorization header: %q", tokenSeen)
	}
}

func TestGitHubFollowersInvalidUser(t *testing.T) {
	t.Parallel()

	client := NewGitHubClient(&http.Client{Timeout: 2 * time.Second}, "")
	_, err := client.Followers(context.Background(), "   ")
	if err == nil || !strings.Contains(err.Error(), "username is empty") {
		t.Fatalf("expected empty username error, got %v", err)
	}
}

func TestGitHubFollowersStatusError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer ts.Close()

	client := NewGitHubClient(&http.Client{Timeout: 2 * time.Second}, "")
	client.baseURL = ts.URL

	_, err := client.Followers(context.Background(), "alice")
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestReadErrorBody(t *testing.T) {
	t.Parallel()

	msg := readErrorBody(strings.NewReader("  hello  "))
	if msg != "hello" {
		t.Fatalf("unexpected message: %q", msg)
	}

	msg = readErrorBody(strings.NewReader("   "))
	if msg != "empty response body" {
		t.Fatalf("unexpected empty-body message: %q", msg)
	}
}
