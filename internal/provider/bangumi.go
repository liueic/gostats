package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

const bangumiBaseURL = "https://api.bgm.tv"

const (
	BangumiSubjectAnime = 2
	BangumiSubjectGame  = 4
)

const (
	BangumiCollectionAll     = 0
	BangumiCollectionWish    = 1
	BangumiCollectionCollect = 2
	BangumiCollectionDo      = 3
	BangumiCollectionOnHold  = 4
	BangumiCollectionDropped = 5
)

type BangumiCollectionSummary struct {
	Username       string
	SubjectType    int
	CollectionType int
	Total          int
}

type BangumiClient struct {
	client      *http.Client
	accessToken string
	baseURL     string
}

func NewBangumiClient(client *http.Client, accessToken string) *BangumiClient {
	return &BangumiClient{
		client:      client,
		accessToken: strings.TrimSpace(accessToken),
		baseURL:     bangumiBaseURL,
	}
}

func (c *BangumiClient) CollectionsTotal(ctx context.Context, username string, subjectType int) (BangumiCollectionSummary, error) {
	return c.CollectionsTotalByType(ctx, username, subjectType, BangumiCollectionAll)
}

func (c *BangumiClient) CollectionsTotalByType(ctx context.Context, username string, subjectType int, collectionType int) (BangumiCollectionSummary, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return BangumiCollectionSummary{}, fmt.Errorf("bangumi username is empty")
	}
	if !isSupportedBangumiSubjectType(subjectType) {
		return BangumiCollectionSummary{}, fmt.Errorf("unsupported bangumi subject type: %d", subjectType)
	}
	if !isSupportedBangumiCollectionType(collectionType) {
		return BangumiCollectionSummary{}, fmt.Errorf("unsupported bangumi collection type: %d", collectionType)
	}

	params := url.Values{}
	params.Set("subject_type", fmt.Sprintf("%d", subjectType))
	params.Set("limit", "1")
	params.Set("offset", "0")
	if collectionType > 0 {
		params.Set("type", fmt.Sprintf("%d", collectionType))
	}
	endpoint := fmt.Sprintf("%s/v0/users/%s/collections?%s", c.baseURL, url.PathEscape(username), params.Encode())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return BangumiCollectionSummary{}, fmt.Errorf("create bangumi request: %w", err)
	}

	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "gostats")
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return BangumiCollectionSummary{}, fmt.Errorf("request bangumi api: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg := readErrorBody(resp.Body)
		return BangumiCollectionSummary{}, fmt.Errorf("bangumi api status %d: %s", resp.StatusCode, msg)
	}

	var payload struct {
		Total int `json:"total"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return BangumiCollectionSummary{}, fmt.Errorf("decode bangumi response: %w", err)
	}

	return BangumiCollectionSummary{
		Username:       username,
		SubjectType:    subjectType,
		CollectionType: collectionType,
		Total:          payload.Total,
	}, nil
}

func isSupportedBangumiSubjectType(subjectType int) bool {
	switch subjectType {
	case BangumiSubjectAnime, BangumiSubjectGame:
		return true
	default:
		return false
	}
}

func isSupportedBangumiCollectionType(collectionType int) bool {
	switch collectionType {
	case BangumiCollectionAll,
		BangumiCollectionWish,
		BangumiCollectionCollect,
		BangumiCollectionDo,
		BangumiCollectionOnHold,
		BangumiCollectionDropped:
		return true
	default:
		return false
	}
}
