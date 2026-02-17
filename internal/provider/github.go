package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const githubBaseURL = "https://api.github.com"

type GitHubClient struct {
	client  *http.Client
	token   string
	baseURL string
}

func NewGitHubClient(client *http.Client, token string) *GitHubClient {
	return &GitHubClient{
		client:  client,
		token:   strings.TrimSpace(token),
		baseURL: githubBaseURL,
	}
}

func (c *GitHubClient) Followers(ctx context.Context, username string) (int, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return 0, fmt.Errorf("github username is empty")
	}

	endpoint := fmt.Sprintf("%s/users/%s", c.baseURL, url.PathEscape(username))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return 0, fmt.Errorf("create github request: %w", err)
	}

	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "gostats")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request github api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := readErrorBody(resp.Body)
		return 0, fmt.Errorf("github api status %d: %s", resp.StatusCode, msg)
	}

	var payload struct {
		Followers int `json:"followers"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return 0, fmt.Errorf("decode github response: %w", err)
	}

	return payload.Followers, nil
}

func readErrorBody(r io.Reader) string {
	raw, err := io.ReadAll(io.LimitReader(r, 512))
	if err != nil {
		return "unable to read response body"
	}

	msg := strings.TrimSpace(string(raw))
	if msg == "" {
		return "empty response body"
	}
	return msg
}
