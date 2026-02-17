package server

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"
)

const (
	spotifyOAuthStateCookieName = "gostats_spotify_oauth_state"
	defaultSpotifyOAuthScopes   = "user-read-currently-playing user-read-recently-played user-library-read"
)

func (s *Server) handleSpotifyAuthStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.spotify == nil || !s.spotify.OAuthConfigured() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "spotify oauth is not configured"})
		return
	}

	redirectURI := s.effectiveSpotifyRedirectURI(r)
	state, err := randomState()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "generate oauth state failed"})
		return
	}

	authURL, err := s.spotify.AuthorizeURL(redirectURI, s.spotifyOAuthScopes, state)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     spotifyOAuthStateCookieName,
		Value:    state,
		Path:     "/spotify/auth",
		HttpOnly: true,
		Secure:   s.requestScheme(r) == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   600,
	})

	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "json") {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":           true,
			"authorizeURL": authURL,
			"redirectURI":  redirectURI,
			"scopes":       s.spotifyOAuthScopes,
		})
		return
	}

	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleSpotifyAuthCallback(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	if s.spotify == nil || !s.spotify.OAuthConfigured() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "spotify oauth is not configured"})
		return
	}

	if apiErr := strings.TrimSpace(r.URL.Query().Get("error")); apiErr != "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "spotify authorize failed: " + apiErr})
		return
	}

	code := strings.TrimSpace(r.URL.Query().Get("code"))
	state := strings.TrimSpace(r.URL.Query().Get("state"))
	if code == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing oauth code"})
		return
	}
	if state == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing oauth state"})
		return
	}

	stateCookie, err := r.Cookie(spotifyOAuthStateCookieName)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "oauth state cookie is missing or expired"})
		return
	}
	if !constantTimeStringEqual(state, stateCookie.Value) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "oauth state mismatch"})
		return
	}

	// Clear one-time state cookie.
	http.SetCookie(w, &http.Cookie{
		Name:     spotifyOAuthStateCookieName,
		Value:    "",
		Path:     "/spotify/auth",
		HttpOnly: true,
		Secure:   s.requestScheme(r) == "https",
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	})

	redirectURI := s.effectiveSpotifyRedirectURI(r)
	refreshToken, err := s.spotify.ExchangeAuthorizationCode(r.Context(), code, redirectURI)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	if _, err := s.spotify.VerifyAndActivateRefreshToken(r.Context(), refreshToken); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "json") {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":                true,
			"message":           "spotify authorization completed",
			"hasRefreshToken":   true,
			"refreshTokenSaved": true,
		})
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte("<h3>Spotify authorization completed. You can close this tab.</h3>"))
}

func (s *Server) handleSpotifyAuthStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	configured := s.spotify != nil && s.spotify.OAuthConfigured()
	hasRefreshToken := false
	if configured {
		ok, err := s.spotify.HasRefreshToken(r.Context())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		hasRefreshToken = ok
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"configured":      configured,
		"hasRefreshToken": hasRefreshToken,
		"redirectURI":     s.effectiveSpotifyRedirectURI(r),
		"scopes":          s.spotifyOAuthScopes,
		"startURL":        "/spotify/auth/start",
	})
}

func (s *Server) effectiveSpotifyRedirectURI(r *http.Request) string {
	if strings.TrimSpace(s.spotifyRedirectURI) != "" {
		return s.spotifyRedirectURI
	}

	host := s.requestHost(r)
	return fmt.Sprintf("%s://%s/spotify/auth/callback", s.requestScheme(r), host)
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func (s *Server) requestScheme(r *http.Request) string {
	if s.trustProxyHeaders {
		if forwardedProto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); forwardedProto != "" {
			parts := strings.Split(forwardedProto, ",")
			if len(parts) > 0 && strings.TrimSpace(parts[0]) != "" {
				return strings.TrimSpace(parts[0])
			}
		}
	}
	if r.TLS != nil {
		return "https"
	}
	if r.URL.Scheme != "" {
		return r.URL.Scheme
	}
	return "http"
}

func (s *Server) requestHost(r *http.Request) string {
	if s.trustProxyHeaders {
		if forwardedHost := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
			parts := strings.Split(forwardedHost, ",")
			if len(parts) > 0 {
				host := strings.TrimSpace(parts[0])
				if host != "" {
					return host
				}
			}
		}
	}

	host := strings.TrimSpace(r.Host)
	if host == "" {
		return "127.0.0.1"
	}
	return host
}

func constantTimeStringEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}
