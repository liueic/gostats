package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode"
)

const steamBaseURL = "https://api.steampowered.com"

type OwnedGamesSummary struct {
	SteamID       string
	GameCount     int
	TotalMinutes  int
	RecentMinutes int
}

type SteamClient struct {
	client  *http.Client
	apiKey  string
	baseURL string
}

func NewSteamClient(client *http.Client, apiKey string) *SteamClient {
	return &SteamClient{
		client:  client,
		apiKey:  strings.TrimSpace(apiKey),
		baseURL: steamBaseURL,
	}
}

func (c *SteamClient) OwnedGamesSummary(ctx context.Context, idOrVanity string) (OwnedGamesSummary, error) {
	if c.apiKey == "" {
		return OwnedGamesSummary{}, fmt.Errorf("STEAM_API_KEY is not configured")
	}

	idOrVanity = strings.TrimSpace(idOrVanity)
	if idOrVanity == "" {
		return OwnedGamesSummary{}, fmt.Errorf("steam id is empty")
	}

	steamID := idOrVanity
	if !looksLikeSteamID64(idOrVanity) {
		resolvedID, err := c.resolveVanityURL(ctx, idOrVanity)
		if err != nil {
			return OwnedGamesSummary{}, err
		}
		steamID = resolvedID
	}

	params := url.Values{}
	params.Set("key", c.apiKey)
	params.Set("steamid", steamID)
	params.Set("include_played_free_games", "true")
	params.Set("format", "json")

	endpoint := fmt.Sprintf("%s/IPlayerService/GetOwnedGames/v0001/?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return OwnedGamesSummary{}, fmt.Errorf("create steam owned games request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return OwnedGamesSummary{}, fmt.Errorf("request steam owned games api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := readErrorBody(resp.Body)
		return OwnedGamesSummary{}, fmt.Errorf("steam api status %d: %s", resp.StatusCode, msg)
	}

	var payload struct {
		Response struct {
			GameCount int `json:"game_count"`
			Games     []struct {
				PlaytimeForever int `json:"playtime_forever"`
				Playtime2Weeks  int `json:"playtime_2weeks"`
			} `json:"games"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return OwnedGamesSummary{}, fmt.Errorf("decode steam response: %w", err)
	}

	totalMinutes := 0
	recentMinutes := 0
	for _, game := range payload.Response.Games {
		totalMinutes += game.PlaytimeForever
		recentMinutes += game.Playtime2Weeks
	}

	return OwnedGamesSummary{
		SteamID:       steamID,
		GameCount:     payload.Response.GameCount,
		TotalMinutes:  totalMinutes,
		RecentMinutes: recentMinutes,
	}, nil
}

func (c *SteamClient) resolveVanityURL(ctx context.Context, vanity string) (string, error) {
	params := url.Values{}
	params.Set("key", c.apiKey)
	params.Set("vanityurl", vanity)

	endpoint := fmt.Sprintf("%s/ISteamUser/ResolveVanityURL/v0001/?%s", c.baseURL, params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return "", fmt.Errorf("create steam vanity request: %w", err)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("request steam vanity api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := readErrorBody(resp.Body)
		return "", fmt.Errorf("steam vanity api status %d: %s", resp.StatusCode, msg)
	}

	var payload struct {
		Response struct {
			SteamID string `json:"steamid"`
			Success int    `json:"success"`
			Message string `json:"message"`
		} `json:"response"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return "", fmt.Errorf("decode steam vanity response: %w", err)
	}

	if payload.Response.Success != 1 || strings.TrimSpace(payload.Response.SteamID) == "" {
		if payload.Response.Message != "" {
			return "", fmt.Errorf("resolve steam vanity failed: %s", payload.Response.Message)
		}
		return "", fmt.Errorf("resolve steam vanity failed: success=%s", strconv.Itoa(payload.Response.Success))
	}

	return payload.Response.SteamID, nil
}

func looksLikeSteamID64(v string) bool {
	if len(v) != 17 {
		return false
	}
	for _, r := range v {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}
