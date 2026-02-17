package main

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func httpResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func TestBuildAuthorizeURL(t *testing.T) {
	t.Parallel()

	link := buildAuthorizeURL("cid", "http://127.0.0.1:8787/callback", "scope-a scope-b", "state1")
	parsed, err := url.Parse(link)
	if err != nil {
		t.Fatalf("parse authorize url: %v", err)
	}
	if parsed.Host != "accounts.spotify.com" || parsed.Path != "/authorize" {
		t.Fatalf("unexpected authorize endpoint: %s", link)
	}

	q := parsed.Query()
	if q.Get("response_type") != "code" {
		t.Fatalf("unexpected response_type: %q", q.Get("response_type"))
	}
	if q.Get("client_id") != "cid" || q.Get("state") != "state1" {
		t.Fatalf("unexpected auth params: %s", parsed.RawQuery)
	}
}

func TestPostTokenRequestBranches(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.Method != http.MethodPost || r.URL.String() != tokenEndpoint {
				t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
			}
			if got := r.Header.Get("Authorization"); got != "Basic "+basicAuth("id", "secret") {
				t.Fatalf("unexpected auth header: %q", got)
			}
			body, _ := io.ReadAll(r.Body)
			values, _ := url.ParseQuery(string(body))
			if values.Get("grant_type") != "refresh_token" {
				t.Fatalf("unexpected grant_type: %q", values.Get("grant_type"))
			}
			return httpResponse(http.StatusOK, `{"access_token":"acc","refresh_token":"rt","expires_in":3600}`), nil
		})}

		payload, err := postTokenRequest(client, "id", "secret", url.Values{
			"grant_type":    {"refresh_token"},
			"refresh_token": {"rt"},
		})
		if err != nil {
			t.Fatalf("postTokenRequest returned error: %v", err)
		}
		if payload.AccessToken != "acc" || payload.RefreshToken != "rt" {
			t.Fatalf("unexpected payload: %+v", payload)
		}
	})

	t.Run("api error payload", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return httpResponse(http.StatusBadRequest, `{"error":"invalid_grant","error_description":"bad code"}`), nil
		})}

		_, err := postTokenRequest(client, "id", "secret", url.Values{"grant_type": {"authorization_code"}})
		if err == nil || !strings.Contains(err.Error(), "invalid_grant") {
			t.Fatalf("expected api payload error, got %v", err)
		}
	})

	t.Run("status without api error payload", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return httpResponse(http.StatusBadGateway, `{"foo":"bar"}`), nil
		})}

		_, err := postTokenRequest(client, "id", "secret", url.Values{"grant_type": {"authorization_code"}})
		if err == nil || !strings.Contains(err.Error(), "status: 502") {
			t.Fatalf("expected status error, got %v", err)
		}
	})

	t.Run("decode error", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return httpResponse(http.StatusOK, `not-json`), nil
		})}

		_, err := postTokenRequest(client, "id", "secret", url.Values{"grant_type": {"authorization_code"}})
		if err == nil || !strings.Contains(err.Error(), "decode token response") {
			t.Fatalf("expected decode error, got %v", err)
		}
	})

	t.Run("request transport error", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New("network down")
		})}

		_, err := postTokenRequest(client, "id", "secret", url.Values{"grant_type": {"authorization_code"}})
		if err == nil || !strings.Contains(err.Error(), "request token endpoint") {
			t.Fatalf("expected transport error, got %v", err)
		}
	})
}

func TestVerifyRefreshTokenBranches(t *testing.T) {
	t.Parallel()

	t.Run("returns rotated refresh token", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			body, _ := io.ReadAll(r.Body)
			values, _ := url.ParseQuery(string(body))
			if values.Get("grant_type") != "refresh_token" || values.Get("refresh_token") != "old" {
				t.Fatalf("unexpected refresh request body: %s", string(body))
			}
			return httpResponse(http.StatusOK, `{"access_token":"acc","refresh_token":"new"}`), nil
		})}

		got, err := verifyRefreshToken(client, "id", "secret", "old")
		if err != nil {
			t.Fatalf("verifyRefreshToken returned error: %v", err)
		}
		if got != "new" {
			t.Fatalf("expected rotated refresh token, got %q", got)
		}
	})

	t.Run("falls back to original refresh token", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return httpResponse(http.StatusOK, `{"access_token":"acc"}`), nil
		})}

		got, err := verifyRefreshToken(client, "id", "secret", "old")
		if err != nil {
			t.Fatalf("verifyRefreshToken returned error: %v", err)
		}
		if got != "old" {
			t.Fatalf("expected fallback refresh token, got %q", got)
		}
	})

	t.Run("empty access token", func(t *testing.T) {
		t.Parallel()

		client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
			return httpResponse(http.StatusOK, `{"access_token":""}`), nil
		})}

		_, err := verifyRefreshToken(client, "id", "secret", "old")
		if err == nil || !strings.Contains(err.Error(), "empty access token") {
			t.Fatalf("expected empty access token error, got %v", err)
		}
	})
}

func TestExchangeCodeUsesAuthorizationCodeGrant(t *testing.T) {
	t.Parallel()

	client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		body, _ := io.ReadAll(r.Body)
		values, _ := url.ParseQuery(string(body))
		if values.Get("grant_type") != "authorization_code" {
			t.Fatalf("unexpected grant_type: %q", values.Get("grant_type"))
		}
		if values.Get("code") != "code123" || values.Get("redirect_uri") != "http://127.0.0.1:8787/callback" {
			t.Fatalf("unexpected exchange params: %s", string(body))
		}
		return httpResponse(http.StatusOK, `{"access_token":"acc","refresh_token":"rt"}`), nil
	})}

	resp, err := exchangeCode(client, "id", "secret", "http://127.0.0.1:8787/callback", "code123")
	if err != nil {
		t.Fatalf("exchangeCode returned error: %v", err)
	}
	if resp.RefreshToken != "rt" {
		t.Fatalf("unexpected token response: %+v", resp)
	}
}

func TestNormalizeListenAddr(t *testing.T) {
	t.Parallel()

	u, _ := url.Parse("http://127.0.0.1/callback")
	if got := normalizeListenAddr(u); got != "127.0.0.1:80" {
		t.Fatalf("unexpected listen addr for http: %q", got)
	}

	u, _ = url.Parse("https://example.com/callback")
	if got := normalizeListenAddr(u); got != "example.com:443" {
		t.Fatalf("unexpected listen addr for https: %q", got)
	}

	u, _ = url.Parse("http://127.0.0.1:8787/callback")
	if got := normalizeListenAddr(u); got != "127.0.0.1:8787" {
		t.Fatalf("unexpected listen addr with explicit port: %q", got)
	}
}

func TestEnvOr(t *testing.T) {
	t.Setenv("SPOTIFY_AUTH_ENV_TEST", " value ")
	if got := envOr("SPOTIFY_AUTH_ENV_TEST", "fallback"); got != "value" {
		t.Fatalf("expected trimmed env value, got %q", got)
	}

	t.Setenv("SPOTIFY_AUTH_ENV_TEST", " ")
	if got := envOr("SPOTIFY_AUTH_ENV_TEST", " fallback "); got != " fallback " {
		t.Fatalf("expected trimmed fallback value, got %q", got)
	}
}
