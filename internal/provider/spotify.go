package provider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const (
	spotifyAPIBaseURL     = "https://api.spotify.com/v1"
	spotifyAccountsAPIURL = "https://accounts.spotify.com/api/token"
	spotifyAuthorizeURL   = "https://accounts.spotify.com/authorize"
)

type SpotifyStatus struct {
	IsPlaying  bool
	TrackName  string
	Artists    []string
	AlbumImage string
	ProgressMS int
	TrackURL   string
	PlayedAt   string
	FromRecent bool
}

type SpotifyClient struct {
	client       *http.Client
	clientID     string
	clientSecret string
	refreshToken string
	store        RefreshTokenStore
	apiBaseURL   string
	accountsURL  string

	mu               sync.Mutex
	accessToken      string
	expiresAt        time.Time
	tokenInitialized bool
}

type spotifyTokenPayload struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Error        string `json:"error"`
	Description  string `json:"error_description"`
}

func NewSpotifyClient(client *http.Client, clientID, clientSecret, refreshToken string, store RefreshTokenStore) *SpotifyClient {
	return &SpotifyClient{
		client:       client,
		clientID:     strings.TrimSpace(clientID),
		clientSecret: strings.TrimSpace(clientSecret),
		refreshToken: strings.TrimSpace(refreshToken),
		store:        store,
		apiBaseURL:   spotifyAPIBaseURL,
		accountsURL:  spotifyAccountsAPIURL,
	}
}

func (c *SpotifyClient) OAuthConfigured() bool {
	return strings.TrimSpace(c.clientID) != "" && strings.TrimSpace(c.clientSecret) != ""
}

func (c *SpotifyClient) AuthorizeURL(redirectURI, scopes, state string) (string, error) {
	if !c.OAuthConfigured() {
		return "", fmt.Errorf("SPOTIFY_CLIENT_ID or SPOTIFY_CLIENT_SECRET is not configured")
	}

	redirectURI = strings.TrimSpace(redirectURI)
	scopes = strings.TrimSpace(scopes)
	state = strings.TrimSpace(state)
	if redirectURI == "" {
		return "", fmt.Errorf("spotify redirect uri is empty")
	}
	if scopes == "" {
		return "", fmt.Errorf("spotify oauth scopes are empty")
	}

	values := url.Values{}
	values.Set("response_type", "code")
	values.Set("client_id", c.clientID)
	values.Set("scope", scopes)
	values.Set("redirect_uri", redirectURI)
	if state != "" {
		values.Set("state", state)
	}

	return spotifyAuthorizeURL + "?" + values.Encode(), nil
}

func (c *SpotifyClient) ExchangeAuthorizationCode(ctx context.Context, code, redirectURI string) (string, error) {
	if !c.OAuthConfigured() {
		return "", fmt.Errorf("SPOTIFY_CLIENT_ID or SPOTIFY_CLIENT_SECRET is not configured")
	}

	code = strings.TrimSpace(code)
	redirectURI = strings.TrimSpace(redirectURI)
	if code == "" {
		return "", fmt.Errorf("spotify oauth code is empty")
	}
	if redirectURI == "" {
		return "", fmt.Errorf("spotify redirect uri is empty")
	}

	values := url.Values{}
	values.Set("grant_type", "authorization_code")
	values.Set("code", code)
	values.Set("redirect_uri", redirectURI)

	payload, err := c.requestToken(ctx, values)
	if err != nil {
		return "", err
	}

	refreshToken := strings.TrimSpace(payload.RefreshToken)
	if refreshToken == "" {
		return "", fmt.Errorf("spotify token api did not return refresh_token")
	}
	return refreshToken, nil
}

func (c *SpotifyClient) VerifyAndActivateRefreshToken(ctx context.Context, refreshToken string) (string, error) {
	if !c.OAuthConfigured() {
		return "", fmt.Errorf("SPOTIFY_CLIENT_ID or SPOTIFY_CLIENT_SECRET is not configured")
	}

	refreshToken = strings.TrimSpace(refreshToken)
	if refreshToken == "" {
		return "", fmt.Errorf("spotify refresh token is empty")
	}

	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", refreshToken)

	payload, err := c.requestToken(ctx, values)
	if err != nil {
		return "", err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	return c.applyTokenPayloadLocked(ctx, payload, refreshToken)
}

func (c *SpotifyClient) HasRefreshToken(ctx context.Context) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if strings.TrimSpace(c.refreshToken) != "" {
		return true, nil
	}

	if c.store == nil {
		return false, nil
	}

	storedToken, err := c.store.Load(ctx)
	if err != nil {
		return false, fmt.Errorf("load refresh token from store: %w", err)
	}
	storedToken = strings.TrimSpace(storedToken)
	if storedToken == "" {
		return false, nil
	}

	c.refreshToken = storedToken
	c.tokenInitialized = true
	return true, nil
}

func (c *SpotifyClient) CurrentStatus(ctx context.Context) (SpotifyStatus, error) {
	if err := c.validateConfig(); err != nil {
		return SpotifyStatus{}, err
	}

	resp, err := c.doAPIRequest(ctx, http.MethodGet, "/me/player/currently-playing", nil)
	if err != nil {
		return SpotifyStatus{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNoContent {
		return c.recentlyPlayedFallback(ctx)
	}
	if resp.StatusCode != http.StatusOK {
		msg := readErrorBody(resp.Body)
		return SpotifyStatus{}, fmt.Errorf("spotify currently-playing api status %d: %s", resp.StatusCode, msg)
	}

	var payload struct {
		IsPlaying  bool `json:"is_playing"`
		ProgressMS int  `json:"progress_ms"`
		Item       *struct {
			Name    string `json:"name"`
			Artists []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Album struct {
				Images []struct {
					URL string `json:"url"`
				} `json:"images"`
			} `json:"album"`
			ExternalURLs struct {
				Spotify string `json:"spotify"`
			} `json:"external_urls"`
		} `json:"item"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return SpotifyStatus{}, fmt.Errorf("decode spotify currently-playing response: %w", err)
	}

	if payload.Item == nil {
		return c.recentlyPlayedFallback(ctx)
	}

	return buildSpotifyStatus(
		payload.IsPlaying,
		payload.Item.Name,
		artistNames(payload.Item.Artists),
		firstImageURL(payload.Item.Album.Images),
		payload.ProgressMS,
		payload.Item.ExternalURLs.Spotify,
		"",
		false,
	), nil
}

func (c *SpotifyClient) SavedTracksCount(ctx context.Context) (int, error) {
	if err := c.validateConfig(); err != nil {
		return 0, err
	}

	params := url.Values{}
	params.Set("limit", "1")

	resp, err := c.doAPIRequest(ctx, http.MethodGet, "/me/tracks", params)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := readErrorBody(resp.Body)
		return 0, fmt.Errorf("spotify saved tracks api status %d: %s", resp.StatusCode, msg)
	}

	var payload struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode spotify saved tracks response: %w", err)
	}

	return payload.Total, nil
}

func (c *SpotifyClient) recentlyPlayedFallback(ctx context.Context) (SpotifyStatus, error) {
	params := url.Values{}
	params.Set("limit", "1")

	resp, err := c.doAPIRequest(ctx, http.MethodGet, "/me/player/recently-played", params)
	if err != nil {
		return SpotifyStatus{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := readErrorBody(resp.Body)
		return SpotifyStatus{}, fmt.Errorf("spotify recently-played api status %d: %s", resp.StatusCode, msg)
	}

	var payload struct {
		Items []struct {
			PlayedAt string `json:"played_at"`
			Track    struct {
				Name    string `json:"name"`
				Artists []struct {
					Name string `json:"name"`
				} `json:"artists"`
				Album struct {
					Images []struct {
						URL string `json:"url"`
					} `json:"images"`
				} `json:"album"`
				ExternalURLs struct {
					Spotify string `json:"spotify"`
				} `json:"external_urls"`
			} `json:"track"`
		} `json:"items"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return SpotifyStatus{}, fmt.Errorf("decode spotify recently-played response: %w", err)
	}

	if len(payload.Items) == 0 {
		return SpotifyStatus{}, fmt.Errorf("spotify recently-played returned empty items")
	}

	last := payload.Items[0]
	return buildSpotifyStatus(
		false,
		last.Track.Name,
		artistNames(last.Track.Artists),
		firstImageURL(last.Track.Album.Images),
		0,
		last.Track.ExternalURLs.Spotify,
		last.PlayedAt,
		true,
	), nil
}

func (c *SpotifyClient) doAPIRequest(ctx context.Context, method, path string, params url.Values) (*http.Response, error) {
	token, err := c.accessTokenForRequest(ctx, false)
	if err != nil {
		return nil, err
	}

	resp, err := c.doAPIRequestWithToken(ctx, method, path, params, token)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusUnauthorized {
		return resp, nil
	}

	resp.Body.Close()

	token, err = c.accessTokenForRequest(ctx, true)
	if err != nil {
		return nil, err
	}
	return c.doAPIRequestWithToken(ctx, method, path, params, token)
}

func (c *SpotifyClient) doAPIRequestWithToken(ctx context.Context, method, path string, params url.Values, token string) (*http.Response, error) {
	endpoint := c.apiBaseURL + path
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("create spotify request: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request spotify api: %w", err)
	}
	return resp, nil
}

func (c *SpotifyClient) accessTokenForRequest(ctx context.Context, forceRefresh bool) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.tokenInitialized {
		if err := c.initRefreshTokenLocked(ctx); err != nil {
			return "", err
		}
		c.tokenInitialized = true
	}

	if !forceRefresh && c.accessToken != "" && time.Now().Before(c.expiresAt.Add(-45*time.Second)) {
		return c.accessToken, nil
	}

	values := url.Values{}
	values.Set("grant_type", "refresh_token")
	values.Set("refresh_token", c.refreshToken)

	payload, err := c.requestToken(ctx, values)
	if err != nil {
		return "", err
	}

	if _, err := c.applyTokenPayloadLocked(ctx, payload, c.refreshToken); err != nil {
		return "", err
	}
	return c.accessToken, nil
}

func (c *SpotifyClient) validateConfig() error {
	if c.clientID == "" {
		return fmt.Errorf("SPOTIFY_CLIENT_ID is not configured")
	}
	if c.clientSecret == "" {
		return fmt.Errorf("SPOTIFY_CLIENT_SECRET is not configured")
	}
	if c.refreshToken == "" && c.store == nil {
		return fmt.Errorf("SPOTIFY_REFRESH_TOKEN is not configured")
	}
	return nil
}

func (c *SpotifyClient) initRefreshTokenLocked(ctx context.Context) error {
	if c.store == nil {
		if c.refreshToken == "" {
			return fmt.Errorf("SPOTIFY_REFRESH_TOKEN is not configured")
		}
		return nil
	}

	storedToken, err := c.store.Load(ctx)
	if err != nil {
		return fmt.Errorf("load refresh token from store: %w", err)
	}
	storedToken = strings.TrimSpace(storedToken)
	if storedToken != "" {
		c.refreshToken = storedToken
		return nil
	}

	if c.refreshToken == "" {
		return fmt.Errorf("SPOTIFY_REFRESH_TOKEN is not configured and refresh token store is empty; authorize once via /spotify/auth/start")
	}

	// Seed store with env-provided token so future rotations can survive restarts.
	if err := c.persistRefreshTokenLocked(ctx, c.refreshToken); err != nil {
		return fmt.Errorf("seed refresh token store: %w", err)
	}
	return nil
}

func (c *SpotifyClient) persistRefreshTokenLocked(ctx context.Context, refreshToken string) error {
	if c.store == nil {
		return nil
	}
	if err := c.store.Save(ctx, strings.TrimSpace(refreshToken)); err != nil {
		return fmt.Errorf("persist refresh token: %w", err)
	}
	return nil
}

func (c *SpotifyClient) requestToken(ctx context.Context, values url.Values) (spotifyTokenPayload, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.accountsURL, strings.NewReader(values.Encode()))
	if err != nil {
		return spotifyTokenPayload{}, fmt.Errorf("create spotify token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Authorization", "Basic "+basicAuth(c.clientID, c.clientSecret))

	resp, err := c.client.Do(req)
	if err != nil {
		return spotifyTokenPayload{}, fmt.Errorf("request spotify token api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := readErrorBody(resp.Body)
		return spotifyTokenPayload{}, fmt.Errorf("spotify token api status %d: %s", resp.StatusCode, msg)
	}

	var payload spotifyTokenPayload
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return spotifyTokenPayload{}, fmt.Errorf("decode spotify token response: %w", err)
	}
	return payload, nil
}

func (c *SpotifyClient) applyTokenPayloadLocked(ctx context.Context, payload spotifyTokenPayload, fallbackRefreshToken string) (string, error) {
	if strings.TrimSpace(payload.AccessToken) == "" {
		return "", fmt.Errorf("spotify token api returned empty access_token")
	}

	expiresIn := payload.ExpiresIn
	if expiresIn <= 0 {
		expiresIn = 3600
	}

	finalRefreshToken := strings.TrimSpace(payload.RefreshToken)
	if finalRefreshToken == "" {
		finalRefreshToken = strings.TrimSpace(fallbackRefreshToken)
	}
	if finalRefreshToken == "" {
		return "", fmt.Errorf("spotify token api returned empty refresh_token")
	}

	if finalRefreshToken != c.refreshToken {
		if err := c.persistRefreshTokenLocked(ctx, finalRefreshToken); err != nil {
			return "", err
		}
	}

	c.refreshToken = finalRefreshToken
	c.accessToken = strings.TrimSpace(payload.AccessToken)
	c.expiresAt = time.Now().Add(time.Duration(expiresIn) * time.Second)
	c.tokenInitialized = true
	return c.refreshToken, nil
}

func basicAuth(clientID, clientSecret string) string {
	raw := clientID + ":" + clientSecret
	return base64.StdEncoding.EncodeToString([]byte(raw))
}

func artistNames(artists []struct {
	Name string `json:"name"`
}) []string {
	names := make([]string, 0, len(artists))
	for _, artist := range artists {
		name := strings.TrimSpace(artist.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return names
}

func firstImageURL(images []struct {
	URL string `json:"url"`
}) string {
	if len(images) == 0 {
		return ""
	}
	return strings.TrimSpace(images[0].URL)
}

func buildSpotifyStatus(isPlaying bool, trackName string, artists []string, albumImage string, progressMS int, trackURL, playedAt string, fromRecent bool) SpotifyStatus {
	return SpotifyStatus{
		IsPlaying:  isPlaying,
		TrackName:  strings.TrimSpace(trackName),
		Artists:    artists,
		AlbumImage: strings.TrimSpace(albumImage),
		ProgressMS: progressMS,
		TrackURL:   strings.TrimSpace(trackURL),
		PlayedAt:   strings.TrimSpace(playedAt),
		FromRecent: fromRecent,
	}
}
