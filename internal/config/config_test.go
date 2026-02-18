package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadMissingFileReturnsDefaults(t *testing.T) {
	t.Parallel()

	cfg, loaded, err := Load(filepath.Join(t.TempDir(), "missing.yml"))
	if err != nil {
		t.Fatalf("load missing file: %v", err)
	}
	if loaded {
		t.Fatalf("expected loaded=false for missing file")
	}
	if cfg.Server.Port != "8080" {
		t.Fatalf("unexpected default port: %q", cfg.Server.Port)
	}
	if cfg.Server.TrustProxyHeaders != "false" {
		t.Fatalf("unexpected trust_proxy_headers default: %q", cfg.Server.TrustProxyHeaders)
	}
	if cfg.Server.ReadHeaderTimeout != "5s" || cfg.Server.ReadTimeout != "15s" || cfg.Server.WriteTimeout != "15s" || cfg.Server.IdleTimeout != "60s" {
		t.Fatalf("unexpected server timeout defaults: %+v", cfg.Server)
	}
	if cfg.Spotify.OAuthScopes == "" {
		t.Fatalf("expected default spotify oauth scopes")
	}
}

func TestLoadParsesYAMLAndExpandsEnv(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yml")
	if err := os.Setenv("TEST_GITHUB_TOKEN", "ghp_test_token"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	if err := os.Setenv("TEST_BANGUMI_ACCESS_TOKEN", "bgm_test_token"); err != nil {
		t.Fatalf("set env: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("TEST_GITHUB_TOKEN")
		_ = os.Unsetenv("TEST_BANGUMI_ACCESS_TOKEN")
	})

	content := `
server:
  port: "9090"
  trust_proxy_headers: "true"
  read_header_timeout: "7s"
cors:
  allowed_origins: "https://blog.example,https://www.blog.example"
github:
  token: "${TEST_GITHUB_TOKEN}"
bangumi:
  access_token: "${TEST_BANGUMI_ACCESS_TOKEN}"
spotify:
  redirect_uri: "https://example.com/spotify/auth/callback"
  oauth_scopes: "user-library-read"
  refresh_token_file: "./token"
`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write config file: %v", err)
	}

	cfg, loaded, err := Load(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if !loaded {
		t.Fatalf("expected loaded=true")
	}
	if cfg.Server.Port != "9090" {
		t.Fatalf("unexpected port: %q", cfg.Server.Port)
	}
	if cfg.Server.TrustProxyHeaders != "true" {
		t.Fatalf("unexpected trust_proxy_headers: %q", cfg.Server.TrustProxyHeaders)
	}
	if cfg.Server.ReadHeaderTimeout != "7s" {
		t.Fatalf("unexpected read_header_timeout: %q", cfg.Server.ReadHeaderTimeout)
	}
	if cfg.CORS.AllowedOrigins != "https://blog.example,https://www.blog.example" {
		t.Fatalf("unexpected cors.allowed_origins: %q", cfg.CORS.AllowedOrigins)
	}
	if cfg.GitHub.Token != "ghp_test_token" {
		t.Fatalf("unexpected github token: %q", cfg.GitHub.Token)
	}
	if cfg.Bangumi.AccessToken != "bgm_test_token" {
		t.Fatalf("unexpected bangumi access token: %q", cfg.Bangumi.AccessToken)
	}
	if cfg.Spotify.RefreshTokenFile != "./token" {
		t.Fatalf("unexpected refresh token file: %q", cfg.Spotify.RefreshTokenFile)
	}
	if cfg.Spotify.RedirectURI != "https://example.com/spotify/auth/callback" {
		t.Fatalf("unexpected spotify redirect uri: %q", cfg.Spotify.RedirectURI)
	}
	if cfg.Spotify.OAuthScopes != "user-library-read" {
		t.Fatalf("unexpected spotify oauth scopes: %q", cfg.Spotify.OAuthScopes)
	}
}

func TestApplyEnvOverrides(t *testing.T) {
	t.Parallel()

	if err := os.Setenv("PORT", "7777"); err != nil {
		t.Fatalf("set env PORT: %v", err)
	}
	if err := os.Setenv("STEAM_API_KEY", "steam_key"); err != nil {
		t.Fatalf("set env STEAM_API_KEY: %v", err)
	}
	if err := os.Setenv("TRUST_PROXY_HEADERS", "true"); err != nil {
		t.Fatalf("set env TRUST_PROXY_HEADERS: %v", err)
	}
	if err := os.Setenv("BANGUMI_ACCESS_TOKEN", "bgm_access_token"); err != nil {
		t.Fatalf("set env BANGUMI_ACCESS_TOKEN: %v", err)
	}
	if err := os.Setenv("CORS_ALLOWED_ORIGINS", "https://blog.example"); err != nil {
		t.Fatalf("set env CORS_ALLOWED_ORIGINS: %v", err)
	}
	if err := os.Setenv("SPOTIFY_REFRESH_TOKEN_FILE", "/tmp/token"); err != nil {
		t.Fatalf("set env SPOTIFY_REFRESH_TOKEN_FILE: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Unsetenv("PORT")
		_ = os.Unsetenv("STEAM_API_KEY")
		_ = os.Unsetenv("TRUST_PROXY_HEADERS")
		_ = os.Unsetenv("BANGUMI_ACCESS_TOKEN")
		_ = os.Unsetenv("CORS_ALLOWED_ORIGINS")
		_ = os.Unsetenv("SPOTIFY_REFRESH_TOKEN_FILE")
	})

	cfg := Default()
	cfg.ApplyEnvOverrides()

	if cfg.Server.Port != "7777" {
		t.Fatalf("unexpected overridden port: %q", cfg.Server.Port)
	}
	if cfg.Steam.APIKey != "steam_key" {
		t.Fatalf("unexpected overridden steam key: %q", cfg.Steam.APIKey)
	}
	if cfg.Server.TrustProxyHeaders != "true" {
		t.Fatalf("unexpected overridden trust_proxy_headers: %q", cfg.Server.TrustProxyHeaders)
	}
	if cfg.Bangumi.AccessToken != "bgm_access_token" {
		t.Fatalf("unexpected overridden bangumi access token: %q", cfg.Bangumi.AccessToken)
	}
	if cfg.CORS.AllowedOrigins != "https://blog.example" {
		t.Fatalf("unexpected overridden cors.allowed_origins: %q", cfg.CORS.AllowedOrigins)
	}
	if cfg.Spotify.RefreshTokenFile != "/tmp/token" {
		t.Fatalf("unexpected overridden refresh token file: %q", cfg.Spotify.RefreshTokenFile)
	}
}
