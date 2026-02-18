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

func TestBangumiCollectionsTotalSuccess(t *testing.T) {
	t.Parallel()

	authSeen := ""
	querySeen := url.Values{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v0/users/alice/collections" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		authSeen = r.Header.Get("Authorization")
		querySeen = r.URL.Query()

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":17,"limit":1,"offset":0,"data":[]}`))
	}))
	defer ts.Close()

	client := NewBangumiClient(&http.Client{Timeout: 2 * time.Second}, "bgm-token")
	client.baseURL = ts.URL

	summary, err := client.CollectionsTotal(context.Background(), "alice", BangumiSubjectAnime)
	if err != nil {
		t.Fatalf("CollectionsTotal returned error: %v", err)
	}
	if summary.Total != 17 {
		t.Fatalf("unexpected total: %d", summary.Total)
	}
	if summary.Username != "alice" || summary.SubjectType != BangumiSubjectAnime || summary.CollectionType != BangumiCollectionAll {
		t.Fatalf("unexpected summary: %+v", summary)
	}
	if authSeen != "Bearer bgm-token" {
		t.Fatalf("unexpected authorization header: %q", authSeen)
	}
	if querySeen.Get("subject_type") != "2" || querySeen.Get("limit") != "1" || querySeen.Get("offset") != "0" {
		t.Fatalf("unexpected query: %v", querySeen)
	}
}

func TestBangumiCollectionsTotalInvalidInput(t *testing.T) {
	t.Parallel()

	client := NewBangumiClient(&http.Client{Timeout: 2 * time.Second}, "")

	_, err := client.CollectionsTotal(context.Background(), "   ", BangumiSubjectAnime)
	if err == nil || !strings.Contains(err.Error(), "username is empty") {
		t.Fatalf("expected empty username error, got %v", err)
	}

	_, err = client.CollectionsTotal(context.Background(), "alice", 99)
	if err == nil || !strings.Contains(err.Error(), "unsupported bangumi subject type") {
		t.Fatalf("expected unsupported type error, got %v", err)
	}

	_, err = client.CollectionsTotalByType(context.Background(), "alice", BangumiSubjectAnime, 99)
	if err == nil || !strings.Contains(err.Error(), "unsupported bangumi collection type") {
		t.Fatalf("expected unsupported collection type error, got %v", err)
	}
}

func TestBangumiCollectionsTotalStatusError(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer ts.Close()

	client := NewBangumiClient(&http.Client{Timeout: 2 * time.Second}, "")
	client.baseURL = ts.URL

	_, err := client.CollectionsTotal(context.Background(), "alice", BangumiSubjectGame)
	if err == nil || !strings.Contains(err.Error(), "status 401") {
		t.Fatalf("expected status error, got %v", err)
	}
}

func TestBangumiCollectionsTotalZeroSuccess(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"total":0,"limit":1,"offset":0,"data":[]}`))
	}))
	defer ts.Close()

	client := NewBangumiClient(&http.Client{Timeout: 2 * time.Second}, "")
	client.baseURL = ts.URL

	summary, err := client.CollectionsTotal(context.Background(), "alice", BangumiSubjectAnime)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if summary.Total != 0 {
		t.Fatalf("expected total 0, got %d", summary.Total)
	}
}

func TestBangumiCollectionsTotalByTypeSuccess(t *testing.T) {
	t.Parallel()

	querySeen := url.Values{}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		querySeen = r.URL.Query()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"total":8,"limit":1,"offset":0,"data":[]}`))
	}))
	defer ts.Close()

	client := NewBangumiClient(&http.Client{Timeout: 2 * time.Second}, "")
	client.baseURL = ts.URL

	summary, err := client.CollectionsTotalByType(context.Background(), "alice", BangumiSubjectAnime, BangumiCollectionDo)
	if err != nil {
		t.Fatalf("CollectionsTotalByType returned error: %v", err)
	}
	if summary.Total != 8 {
		t.Fatalf("unexpected total: %d", summary.Total)
	}
	if summary.CollectionType != BangumiCollectionDo {
		t.Fatalf("unexpected collection type in summary: %+v", summary)
	}
	if querySeen.Get("type") != "3" {
		t.Fatalf("expected query type=3, got %q", querySeen.Get("type"))
	}
	if querySeen.Get("subject_type") != "2" {
		t.Fatalf("expected query subject_type=2, got %q", querySeen.Get("subject_type"))
	}
}
