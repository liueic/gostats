package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"gostats/internal/config"
	"gostats/internal/provider"
	"gostats/internal/server"
)

func main() {
	configPath := getEnv("CONFIG_FILE", "config.yml")
	cfg, loaded, err := config.Load(configPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	cfg.ApplyEnvOverrides()

	if loaded {
		log.Printf("Loaded config file: %s", configPath)
	} else {
		log.Printf("Config file not found: %s (using defaults + env)", configPath)
	}

	port := cfg.Server.Port
	githubToken := cfg.GitHub.Token
	steamAPIKey := cfg.Steam.APIKey
	bangumiAccessToken := cfg.Bangumi.AccessToken
	spotifyClientID := cfg.Spotify.ClientID
	spotifyClientSecret := cfg.Spotify.ClientSecret
	spotifyRedirectURI := cfg.Spotify.RedirectURI
	spotifyOAuthScopes := cfg.Spotify.OAuthScopes
	spotifyRefreshToken := cfg.Spotify.RefreshToken
	spotifyRefreshTokenFile := effectiveSpotifyRefreshTokenFile(
		spotifyClientID,
		spotifyClientSecret,
		cfg.Spotify.RefreshTokenFile,
	)
	spotifyRefreshPersistCmd := cfg.Spotify.RefreshTokenPersistCmd
	cacheTTL := parseDurationOrDefault(cfg.Cache.TTL, 5*time.Minute)
	httpTimeout := parseDurationOrDefault(cfg.HTTP.Timeout, 10*time.Second)
	trustProxyHeaders := parseBoolOrDefault(cfg.Server.TrustProxyHeaders, false)
	serverReadHeaderTimeout := parseDurationOrDefault(cfg.Server.ReadHeaderTimeout, 5*time.Second)
	serverReadTimeout := parseDurationOrDefault(cfg.Server.ReadTimeout, 15*time.Second)
	serverWriteTimeout := parseDurationOrDefault(cfg.Server.WriteTimeout, 15*time.Second)
	serverIdleTimeout := parseDurationOrDefault(cfg.Server.IdleTimeout, 60*time.Second)
	corsAllowedOrigins := parseCSV(cfg.CORS.AllowedOrigins)

	client := &http.Client{Timeout: httpTimeout}
	githubClient := provider.NewGitHubClient(client, githubToken)
	steamClient := provider.NewSteamClient(client, steamAPIKey)
	bangumiClient := provider.NewBangumiClient(client, bangumiAccessToken)
	stores := make([]provider.RefreshTokenStore, 0, 2)
	if spotifyRefreshTokenFile != "" {
		stores = append(stores, provider.NewFileRefreshTokenStore(spotifyRefreshTokenFile))
	}
	if spotifyRefreshPersistCmd != "" {
		stores = append(stores, provider.NewCommandRefreshTokenStore(spotifyRefreshPersistCmd))
	}

	var spotifyStore provider.RefreshTokenStore
	switch len(stores) {
	case 0:
		spotifyStore = nil
	case 1:
		spotifyStore = stores[0]
	default:
		spotifyStore = provider.NewMultiRefreshTokenStore(stores...)
	}

	spotifyClient := provider.NewSpotifyClient(client, spotifyClientID, spotifyClientSecret, spotifyRefreshToken, spotifyStore)
	srv := server.New(githubClient, steamClient, spotifyClient, bangumiClient, server.Options{
		CacheTTL:           cacheTTL,
		SpotifyRedirectURI: spotifyRedirectURI,
		SpotifyOAuthScopes: spotifyOAuthScopes,
		TrustProxyHeaders:  trustProxyHeaders,
		CORSAllowedOrigins: corsAllowedOrigins,
	})

	if steamAPIKey == "" {
		log.Printf("STEAM_API_KEY is empty. Steam endpoints will return failed=true")
	}
	if bangumiAccessToken == "" {
		log.Printf("BANGUMI_ACCESS_TOKEN is empty. Bangumi endpoints can only access public data")
	}
	if spotifyClientID == "" || spotifyClientSecret == "" {
		log.Printf("Spotify oauth config is incomplete. Set client_id/client_secret to enable spotify endpoints")
	} else if spotifyRefreshToken == "" && spotifyRefreshTokenFile == "" {
		log.Printf("Spotify refresh token is not configured yet. Authorize once via /spotify/auth/start")
	}
	if spotifyRefreshTokenFile != "" {
		log.Printf("Spotify refresh token persistence enabled: %s", spotifyRefreshTokenFile)
	}
	if spotifyRefreshPersistCmd != "" {
		log.Printf("Spotify refresh token persist command enabled")
	}
	if trustProxyHeaders {
		log.Printf("Trusted proxy headers enabled (X-Forwarded-Proto/X-Forwarded-Host)")
	}
	if len(corsAllowedOrigins) == 0 {
		log.Printf("CORS allowed origins: none (cross-origin browser requests are blocked)")
	} else {
		log.Printf("CORS allowed origins: %s", strings.Join(corsAllowedOrigins, ","))
	}

	addr := ":" + port
	log.Printf("gostats listening on %s", addr)
	httpSrv := &http.Server{
		Addr:              addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: serverReadHeaderTimeout,
		ReadTimeout:       serverReadTimeout,
		WriteTimeout:      serverWriteTimeout,
		IdleTimeout:       serverIdleTimeout,
	}
	if err := httpSrv.ListenAndServe(); err != nil {
		log.Fatal(err)
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func parseDurationOrDefault(value string, fallback time.Duration) time.Duration {
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return fallback
	}
	return parsed
}

func parseBoolOrDefault(value string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "t", "yes", "y", "on":
		return true
	case "0", "false", "f", "no", "n", "off":
		return false
	default:
		return fallback
	}
}

func parseCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}

func effectiveSpotifyRefreshTokenFile(clientID, clientSecret, configuredPath string) string {
	path := strings.TrimSpace(configuredPath)
	if path != "" {
		return path
	}
	if strings.TrimSpace(clientID) == "" || strings.TrimSpace(clientSecret) == "" {
		return ""
	}
	// Enable persistence by default once Spotify OAuth is configured.
	return "./data/spotify_refresh_token"
}
