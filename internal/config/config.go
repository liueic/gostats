package config

import (
	"fmt"
	"os"
	"strings"
)

type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Cache   CacheConfig   `yaml:"cache"`
	HTTP    HTTPConfig    `yaml:"http"`
	CORS    CORSConfig    `yaml:"cors"`
	GitHub  GitHubConfig  `yaml:"github"`
	Steam   SteamConfig   `yaml:"steam"`
	Spotify SpotifyConfig `yaml:"spotify"`
}

type ServerConfig struct {
	Port              string `yaml:"port"`
	TrustProxyHeaders string `yaml:"trust_proxy_headers"`
	ReadHeaderTimeout string `yaml:"read_header_timeout"`
	ReadTimeout       string `yaml:"read_timeout"`
	WriteTimeout      string `yaml:"write_timeout"`
	IdleTimeout       string `yaml:"idle_timeout"`
}

type CacheConfig struct {
	TTL string `yaml:"ttl"`
}

type HTTPConfig struct {
	Timeout string `yaml:"timeout"`
}

type CORSConfig struct {
	AllowedOrigins string `yaml:"allowed_origins"`
}

type GitHubConfig struct {
	Token string `yaml:"token"`
}

type SteamConfig struct {
	APIKey string `yaml:"api_key"`
}

type SpotifyConfig struct {
	ClientID               string `yaml:"client_id"`
	ClientSecret           string `yaml:"client_secret"`
	RedirectURI            string `yaml:"redirect_uri"`
	OAuthScopes            string `yaml:"oauth_scopes"`
	RefreshToken           string `yaml:"refresh_token"`
	RefreshTokenFile       string `yaml:"refresh_token_file"`
	RefreshTokenPersistCmd string `yaml:"refresh_token_persist_cmd"`
}

func Default() Config {
	return Config{
		Server: ServerConfig{
			Port:              "8080",
			TrustProxyHeaders: "false",
			ReadHeaderTimeout: "5s",
			ReadTimeout:       "15s",
			WriteTimeout:      "15s",
			IdleTimeout:       "60s",
		},
		Cache: CacheConfig{
			TTL: "5m",
		},
		HTTP: HTTPConfig{
			Timeout: "10s",
		},
		Spotify: SpotifyConfig{
			OAuthScopes: "user-read-currently-playing user-read-recently-played user-library-read",
		},
	}
}

// Load reads YAML config from path. If file does not exist, defaults are returned.
func Load(path string) (cfg Config, loaded bool, err error) {
	cfg = Default()

	path = strings.TrimSpace(path)
	if path == "" {
		return cfg, false, nil
	}

	raw, readErr := os.ReadFile(path)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return cfg, false, nil
		}
		return cfg, false, fmt.Errorf("read config file %s: %w", path, readErr)
	}

	expanded := os.ExpandEnv(string(raw))
	if err := unmarshalYAML([]byte(expanded), &cfg); err != nil {
		return cfg, false, fmt.Errorf("parse config file %s: %w", path, err)
	}

	cfg.ApplyDefaults()
	return cfg, true, nil
}

func (c *Config) ApplyDefaults() {
	def := Default()

	if strings.TrimSpace(c.Server.Port) == "" {
		c.Server.Port = def.Server.Port
	}
	if strings.TrimSpace(c.Server.TrustProxyHeaders) == "" {
		c.Server.TrustProxyHeaders = def.Server.TrustProxyHeaders
	}
	if strings.TrimSpace(c.Server.ReadHeaderTimeout) == "" {
		c.Server.ReadHeaderTimeout = def.Server.ReadHeaderTimeout
	}
	if strings.TrimSpace(c.Server.ReadTimeout) == "" {
		c.Server.ReadTimeout = def.Server.ReadTimeout
	}
	if strings.TrimSpace(c.Server.WriteTimeout) == "" {
		c.Server.WriteTimeout = def.Server.WriteTimeout
	}
	if strings.TrimSpace(c.Server.IdleTimeout) == "" {
		c.Server.IdleTimeout = def.Server.IdleTimeout
	}
	if strings.TrimSpace(c.Cache.TTL) == "" {
		c.Cache.TTL = def.Cache.TTL
	}
	if strings.TrimSpace(c.HTTP.Timeout) == "" {
		c.HTTP.Timeout = def.HTTP.Timeout
	}
	if strings.TrimSpace(c.Spotify.OAuthScopes) == "" {
		c.Spotify.OAuthScopes = def.Spotify.OAuthScopes
	}
}

// ApplyEnvOverrides allows env vars to override yaml values.
func (c *Config) ApplyEnvOverrides() {
	c.Server.Port = envOr("PORT", c.Server.Port)
	c.Server.TrustProxyHeaders = envOr("TRUST_PROXY_HEADERS", c.Server.TrustProxyHeaders)
	c.Server.ReadHeaderTimeout = envOr("SERVER_READ_HEADER_TIMEOUT", c.Server.ReadHeaderTimeout)
	c.Server.ReadTimeout = envOr("SERVER_READ_TIMEOUT", c.Server.ReadTimeout)
	c.Server.WriteTimeout = envOr("SERVER_WRITE_TIMEOUT", c.Server.WriteTimeout)
	c.Server.IdleTimeout = envOr("SERVER_IDLE_TIMEOUT", c.Server.IdleTimeout)
	c.Cache.TTL = envOr("CACHE_TTL", c.Cache.TTL)
	c.HTTP.Timeout = envOr("HTTP_TIMEOUT", c.HTTP.Timeout)
	c.CORS.AllowedOrigins = envOr("CORS_ALLOWED_ORIGINS", c.CORS.AllowedOrigins)

	c.GitHub.Token = envOr("GITHUB_TOKEN", c.GitHub.Token)
	c.Steam.APIKey = envOr("STEAM_API_KEY", c.Steam.APIKey)

	c.Spotify.ClientID = envOr("SPOTIFY_CLIENT_ID", c.Spotify.ClientID)
	c.Spotify.ClientSecret = envOr("SPOTIFY_CLIENT_SECRET", c.Spotify.ClientSecret)
	c.Spotify.RedirectURI = envOr("SPOTIFY_REDIRECT_URI", c.Spotify.RedirectURI)
	c.Spotify.OAuthScopes = envOr("SPOTIFY_OAUTH_SCOPES", c.Spotify.OAuthScopes)
	c.Spotify.RefreshToken = envOr("SPOTIFY_REFRESH_TOKEN", c.Spotify.RefreshToken)
	c.Spotify.RefreshTokenFile = envOr("SPOTIFY_REFRESH_TOKEN_FILE", c.Spotify.RefreshTokenFile)
	c.Spotify.RefreshTokenPersistCmd = envOr("SPOTIFY_REFRESH_TOKEN_PERSIST_CMD", c.Spotify.RefreshTokenPersistCmd)

	c.ApplyDefaults()
}

func envOr(key, fallback string) string {
	value := strings.TrimSpace(os.Getenv(key))
	if value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}
