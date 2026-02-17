package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"gostats/internal/provider"
)

const (
	defaultRedirectURI = "http://127.0.0.1:8787/callback"
	defaultScopes      = "user-read-currently-playing user-read-recently-played user-library-read"
	tokenEndpoint      = "https://accounts.spotify.com/api/token"
	authEndpoint       = "https://accounts.spotify.com/authorize"
)

type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	Scope        string `json:"scope"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func main() {
	clientID := flag.String("client-id", envOr("SPOTIFY_CLIENT_ID", ""), "Spotify client id")
	clientSecret := flag.String("client-secret", envOr("SPOTIFY_CLIENT_SECRET", ""), "Spotify client secret")
	redirectURI := flag.String("redirect-uri", envOr("SPOTIFY_REDIRECT_URI", defaultRedirectURI), "Spotify redirect uri (must match app settings)")
	refreshTokenFile := flag.String("refresh-token-file", envOr("SPOTIFY_REFRESH_TOKEN_FILE", ""), "Optional file path to persist refresh token")
	scopes := flag.String("scopes", defaultScopes, "OAuth scopes, space-separated")
	timeout := flag.Duration("timeout", 5*time.Minute, "Authorization timeout")
	flag.Parse()

	if strings.TrimSpace(*clientID) == "" || strings.TrimSpace(*clientSecret) == "" {
		log.Fatalf("missing credentials: set --client-id/--client-secret or SPOTIFY_CLIENT_ID/SPOTIFY_CLIENT_SECRET")
	}

	redirectURL, err := url.Parse(*redirectURI)
	if err != nil {
		log.Fatalf("invalid redirect uri: %v", err)
	}
	if redirectURL.Scheme != "http" && redirectURL.Scheme != "https" {
		log.Fatalf("redirect uri scheme must be http/https")
	}
	if strings.TrimSpace(redirectURL.Host) == "" || strings.TrimSpace(redirectURL.Path) == "" {
		log.Fatalf("redirect uri must include host and path")
	}

	state, err := randomState()
	if err != nil {
		log.Fatalf("generate state: %v", err)
	}

	authURL := buildAuthorizeURL(*clientID, *redirectURI, *scopes, state)
	log.Printf("Open this URL in your browser and approve access:\n%s", authURL)

	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)

	srv := &http.Server{
		Addr:              normalizeListenAddr(redirectURL),
		ReadHeaderTimeout: 5 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != redirectURL.Path {
				http.NotFound(w, r)
				return
			}
			query := r.URL.Query()
			if apiErr := strings.TrimSpace(query.Get("error")); apiErr != "" {
				http.Error(w, "Spotify authorization failed: "+apiErr, http.StatusBadRequest)
				select {
				case errCh <- fmt.Errorf("spotify authorize error: %s", apiErr):
				default:
				}
				return
			}
			if query.Get("state") != state {
				http.Error(w, "invalid state", http.StatusBadRequest)
				select {
				case errCh <- fmt.Errorf("state mismatch"):
				default:
				}
				return
			}
			code := strings.TrimSpace(query.Get("code"))
			if code == "" {
				http.Error(w, "missing code", http.StatusBadRequest)
				select {
				case errCh <- fmt.Errorf("callback missing code"):
				default:
				}
				return
			}

			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<h3>Spotify authorization complete. You can close this tab.</h3>"))
			select {
			case codeCh <- code:
			default:
			}
		}),
	}

	listener, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		log.Fatalf("listen callback server (%s): %v", srv.Addr, err)
	}
	defer listener.Close()

	go func() {
		if serveErr := srv.Serve(listener); serveErr != nil && serveErr != http.ErrServerClosed {
			errCh <- fmt.Errorf("callback server: %w", serveErr)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	timer := time.NewTimer(*timeout)
	defer timer.Stop()

	var code string
	select {
	case <-ctx.Done():
		log.Fatalf("cancelled: %v", ctx.Err())
	case <-timer.C:
		log.Fatalf("timed out waiting for callback after %s", timeout.String())
	case err := <-errCh:
		log.Fatalf("authorization callback error: %v", err)
	case code = <-codeCh:
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = srv.Shutdown(shutdownCtx)
	cancel()

	httpClient := &http.Client{Timeout: 10 * time.Second}
	initial, err := exchangeCode(httpClient, *clientID, *clientSecret, *redirectURI, code)
	if err != nil {
		log.Fatalf("exchange code failed: %v", err)
	}
	if strings.TrimSpace(initial.RefreshToken) == "" {
		log.Fatalf("spotify did not return refresh_token. revoke app access and re-authorize to regenerate")
	}

	finalRefresh, err := verifyRefreshToken(httpClient, *clientID, *clientSecret, initial.RefreshToken)
	if err != nil {
		log.Fatalf("refresh token verification failed: %v", err)
	}

	if strings.TrimSpace(*refreshTokenFile) != "" {
		store := provider.NewFileRefreshTokenStore(*refreshTokenFile)
		if err := store.Save(context.Background(), finalRefresh); err != nil {
			log.Fatalf("persist refresh token to file failed: %v", err)
		}
		log.Printf("Refresh token persisted to %s", *refreshTokenFile)
	}

	log.Println("Spotify refresh token is valid and can directly refresh access token.")
	fmt.Println()
	fmt.Println("Copy and run:")
	fmt.Printf("export SPOTIFY_CLIENT_ID=%q\n", *clientID)
	fmt.Printf("export SPOTIFY_CLIENT_SECRET=%q\n", *clientSecret)
	fmt.Printf("export SPOTIFY_REFRESH_TOKEN=%q\n", finalRefresh)
	if strings.TrimSpace(*refreshTokenFile) != "" {
		fmt.Printf("export SPOTIFY_REFRESH_TOKEN_FILE=%q\n", *refreshTokenFile)
	}
}

func buildAuthorizeURL(clientID, redirectURI, scopes, state string) string {
	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", clientID)
	values.Set("scope", scopes)
	values.Set("redirect_uri", redirectURI)
	values.Set("state", state)
	return authEndpoint + "?" + values.Encode()
}

func exchangeCode(client *http.Client, clientID, clientSecret, redirectURI, code string) (tokenResponse, error) {
	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)
	return postTokenRequest(client, clientID, clientSecret, values)
}

func verifyRefreshToken(client *http.Client, clientID, clientSecret, refreshToken string) (string, error) {
	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)

	resp, err := postTokenRequest(client, clientID, clientSecret, values)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(resp.AccessToken) == "" {
		return "", fmt.Errorf("empty access token from refresh flow")
	}

	if strings.TrimSpace(resp.RefreshToken) != "" {
		return resp.RefreshToken, nil
	}
	return refreshToken, nil
}

func postTokenRequest(client *http.Client, clientID, clientSecret string, values url.Values) (tokenResponse, error) {
	req, err := http.NewRequest(http.MethodPost, tokenEndpoint, strings.NewReader(values.Encode()))
	if err != nil {
		return tokenResponse{}, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+basicAuth(clientID, clientSecret))

	resp, err := client.Do(req)
	if err != nil {
		return tokenResponse{}, fmt.Errorf("request token endpoint: %w", err)
	}
	defer resp.Body.Close()

	var payload tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return tokenResponse{}, fmt.Errorf("decode token response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		if payload.Error != "" {
			return tokenResponse{}, fmt.Errorf("spotify token error: %s (%s)", payload.Error, payload.Description)
		}
		return tokenResponse{}, fmt.Errorf("spotify token endpoint status: %d", resp.StatusCode)
	}
	return payload, nil
}

func basicAuth(clientID, clientSecret string) string {
	raw := clientID + ":" + clientSecret
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func randomState() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func normalizeListenAddr(redirectURL *url.URL) string {
	host := redirectURL.Host
	if strings.Contains(host, ":") {
		return host
	}

	if redirectURL.Scheme == "https" {
		return host + ":443"
	}
	return host + ":80"
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
