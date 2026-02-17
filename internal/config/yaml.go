package config

import (
	"fmt"
	"strings"
)

// unmarshalYAML parses a small YAML subset that is enough for this project's config.yml.
// Supported:
// - comments with #
// - two-level maps: section -> key: value
// - quoted or plain scalar string values
func unmarshalYAML(data []byte, out any) error {
	cfg, ok := out.(*Config)
	if !ok {
		return fmt.Errorf("unsupported yaml target type")
	}
	return parseProjectConfigYAML(string(data), cfg)
}

func parseProjectConfigYAML(raw string, cfg *Config) error {
	lines := strings.Split(raw, "\n")
	section := ""

	for i, line := range lines {
		lineNo := i + 1
		trimmedLine := strings.TrimRight(line, " \t")
		if trimmedLine == "" {
			continue
		}

		content := stripComment(trimmedLine)
		if strings.TrimSpace(content) == "" {
			continue
		}

		indent := leadingSpaces(content)
		trimmed := strings.TrimSpace(content)

		if indent == 0 {
			if !strings.HasSuffix(trimmed, ":") {
				return fmt.Errorf("line %d: expected section like 'name:'", lineNo)
			}
			section = strings.TrimSpace(strings.TrimSuffix(trimmed, ":"))
			continue
		}

		if indent != 2 {
			return fmt.Errorf("line %d: only one nested level is supported", lineNo)
		}
		if section == "" {
			return fmt.Errorf("line %d: key outside section", lineNo)
		}

		idx := strings.Index(trimmed, ":")
		if idx <= 0 {
			return fmt.Errorf("line %d: expected key:value", lineNo)
		}

		key := strings.TrimSpace(trimmed[:idx])
		value := strings.TrimSpace(trimmed[idx+1:])
		value = parseScalar(value)

		assignConfigValue(cfg, section, key, value)
	}

	return nil
}

func assignConfigValue(cfg *Config, section, key, value string) {
	switch section {
	case "server":
		switch key {
		case "port":
			cfg.Server.Port = value
		case "trust_proxy_headers":
			cfg.Server.TrustProxyHeaders = value
		case "read_header_timeout":
			cfg.Server.ReadHeaderTimeout = value
		case "read_timeout":
			cfg.Server.ReadTimeout = value
		case "write_timeout":
			cfg.Server.WriteTimeout = value
		case "idle_timeout":
			cfg.Server.IdleTimeout = value
		}
	case "cache":
		if key == "ttl" {
			cfg.Cache.TTL = value
		}
	case "http":
		if key == "timeout" {
			cfg.HTTP.Timeout = value
		}
	case "cors":
		if key == "allowed_origins" {
			cfg.CORS.AllowedOrigins = value
		}
	case "github":
		if key == "token" {
			cfg.GitHub.Token = value
		}
	case "steam":
		if key == "api_key" {
			cfg.Steam.APIKey = value
		}
	case "spotify":
		switch key {
		case "client_id":
			cfg.Spotify.ClientID = value
		case "client_secret":
			cfg.Spotify.ClientSecret = value
		case "redirect_uri":
			cfg.Spotify.RedirectURI = value
		case "oauth_scopes":
			cfg.Spotify.OAuthScopes = value
		case "refresh_token":
			cfg.Spotify.RefreshToken = value
		case "refresh_token_file":
			cfg.Spotify.RefreshTokenFile = value
		case "refresh_token_persist_cmd":
			cfg.Spotify.RefreshTokenPersistCmd = value
		}
	}
}

func parseScalar(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}

	if len(v) >= 2 {
		if (v[0] == '"' && v[len(v)-1] == '"') || (v[0] == '\'' && v[len(v)-1] == '\'') {
			return v[1 : len(v)-1]
		}
	}
	return v
}

func stripComment(line string) string {
	inSingle := false
	inDouble := false

	for i, r := range line {
		switch r {
		case '\'':
			if !inDouble {
				inSingle = !inSingle
			}
		case '"':
			if !inSingle {
				inDouble = !inDouble
			}
		case '#':
			if !inSingle && !inDouble {
				return strings.TrimRight(line[:i], " \t")
			}
		}
	}
	return line
}

func leadingSpaces(s string) int {
	count := 0
	for _, r := range s {
		if r == ' ' {
			count++
			continue
		}
		break
	}
	return count
}
