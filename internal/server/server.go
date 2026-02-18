package server

import (
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gostats/internal/cache"
	"gostats/internal/model"
	"gostats/internal/provider"
)

type Server struct {
	github             *provider.GitHubClient
	steam              *provider.SteamClient
	spotify            *provider.SpotifyClient
	bangumi            *provider.BangumiClient
	spotifyRedirectURI string
	spotifyOAuthScopes string
	trustProxyHeaders  bool
	corsAllowAll       bool
	corsAllowedOrigins map[string]struct{}
	cache              *cache.Memory
}

type Options struct {
	CacheTTL           time.Duration
	SpotifyRedirectURI string
	SpotifyOAuthScopes string
	TrustProxyHeaders  bool
	CORSAllowedOrigins []string
}

func New(github *provider.GitHubClient, steam *provider.SteamClient, spotify *provider.SpotifyClient, bangumi *provider.BangumiClient, opts Options) *Server {
	if opts.CacheTTL <= 0 {
		opts.CacheTTL = 5 * time.Minute
	}
	if strings.TrimSpace(opts.SpotifyOAuthScopes) == "" {
		opts.SpotifyOAuthScopes = defaultSpotifyOAuthScopes
	}
	corsAllowAll, corsAllowedOrigins := normalizeCORSOrigins(opts.CORSAllowedOrigins)
	return &Server{
		github:             github,
		steam:              steam,
		spotify:            spotify,
		bangumi:            bangumi,
		spotifyRedirectURI: strings.TrimSpace(opts.SpotifyRedirectURI),
		spotifyOAuthScopes: strings.TrimSpace(opts.SpotifyOAuthScopes),
		trustProxyHeaders:  opts.TrustProxyHeaders,
		corsAllowAll:       corsAllowAll,
		corsAllowedOrigins: corsAllowedOrigins,
		cache:              cache.NewMemory(opts.CacheTTL),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", s.handleHealth)
	mux.HandleFunc("/spotify/auth/start", s.handleSpotifyAuthStart)
	mux.HandleFunc("/spotify/auth/callback", s.handleSpotifyAuthCallback)
	mux.HandleFunc("/spotify/auth/status", s.handleSpotifyAuthStatus)
	mux.HandleFunc("/stats.json", s.handleBatchStats)
	mux.HandleFunc("/stats/", s.handleStatsBySource)
	mux.HandleFunc("/", s.handleIndex)
	return s.withCORS(mux)
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "json") {
		s.indexJSONResponse(w)
		return
	}

	writeIndexPage(w)
}

func indexEndpoints() []string {
	return []string{
		"/healthz",
		"/spotify/auth/start",
		"/spotify/auth/callback",
		"/spotify/auth/status",
		"/stats/github/:username",
		"/stats/steamgames/:steamid_or_vanity",
		"/stats/steamtime/:steamid_or_vanity",
		"/stats/steam2weekstime/:steamid_or_vanity",
		"/stats/spotifyplaying/:key",
		"/stats/spotifysaved/:key",
		"/stats/bangumianime/:username",
		"/stats/bangumigame/:username",
		"/stats/bangumianimewatching/:username",
		"/stats/bangumianimewatched/:username",
		"/stats/bangumianimewish/:username",
		"/stats/bangumigameplaying/:username",
		"/stats/bangumigameplayed/:username",
		"/stats/bangumigamewish/:username",
		"/stats.json?github=:username&steam=:steamid_or_vanity&spotify=:key&bangumi=:username",
	}
}

func (s *Server) indexJSONResponse(w http.ResponseWriter) {
	writeJSON(w, http.StatusOK, map[string]any{
		"name":        "gostats",
		"description": "Stat endpoints for static blog clients",
		"endpoints":   indexEndpoints(),
	})
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleStatsBySource(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	trimmed := strings.TrimPrefix(r.URL.Path, "/stats/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "expected /stats/:source/:key"})
		return
	}

	source := strings.ToLower(parts[0])
	key, err := url.PathUnescape(parts[1])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid key path"})
		return
	}

	var stat model.StatResponse
	switch source {
	case "github":
		stat = s.githubFollowersStat(r, key)
	case "steamgames":
		stat = s.steamGamesStat(r, key)
	case "steamtime":
		stat = s.steamPlaytimeStat(r, key)
	case "steam2weekstime":
		stat = s.steamRecentPlaytimeStat(r, key)
	case "spotifyplaying":
		stat = s.spotifyPlayingStat(r, key)
	case "spotifysaved":
		stat = s.spotifySavedTracksStat(r, key)
	case "bangumianime":
		stat = s.bangumiAnimeCollectionsStat(r, key)
	case "bangumigame":
		stat = s.bangumiGameCollectionsStat(r, key)
	case "bangumianimewatching":
		stat = s.bangumiAnimeWatchingStat(r, key)
	case "bangumianimewatched":
		stat = s.bangumiAnimeWatchedStat(r, key)
	case "bangumianimewish":
		stat = s.bangumiAnimeWishStat(r, key)
	case "bangumigameplaying":
		stat = s.bangumiGamePlayingStat(r, key)
	case "bangumigameplayed":
		stat = s.bangumiGamePlayedStat(r, key)
	case "bangumigamewish":
		stat = s.bangumiGameWishStat(r, key)
	default:
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "unsupported source"})
		return
	}

	writeJSON(w, http.StatusOK, stat)
}

func (s *Server) handleBatchStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	githubKey := strings.TrimSpace(r.URL.Query().Get("github"))
	steamKey := strings.TrimSpace(r.URL.Query().Get("steam"))
	spotifyKey := strings.TrimSpace(r.URL.Query().Get("spotify"))
	bangumiKey := strings.TrimSpace(r.URL.Query().Get("bangumi"))
	if githubKey == "" && steamKey == "" && spotifyKey == "" && bangumiKey == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "provide query github and/or steam and/or spotify and/or bangumi"})
		return
	}

	stats := make([]model.StatResponse, 0, 14)
	if githubKey != "" {
		stats = append(stats, s.githubFollowersStat(r, githubKey))
	}

	if steamKey != "" {
		summary, err := s.steamSummaryCached(r, steamKey)
		if err != nil {
			stats = append(stats,
				newErrStat("steamgames", steamKey, "games", "Steam Games", "games", err),
				newErrStat("steamtime", steamKey, "playtime", "Steam Playtime", "hours", err),
				newErrStat("steam2weekstime", steamKey, "playtime_2weeks", "Steam Playtime (2 Weeks)", "hours", err),
			)
		} else {
			hours := round1(float64(summary.TotalMinutes) / 60.0)
			recentHours := round1(float64(summary.RecentMinutes) / 60.0)
			stats = append(stats,
				newOKStat("steamgames", steamKey, "games", "Steam Games", summary.GameCount, "games"),
				newOKStat("steamtime", steamKey, "playtime", "Steam Playtime", hours, "hours"),
				newOKStat("steam2weekstime", steamKey, "playtime_2weeks", "Steam Playtime (2 Weeks)", recentHours, "hours"),
			)
		}
	}

	if spotifyKey != "" {
		stats = append(stats,
			s.spotifyPlayingStat(r, spotifyKey),
			s.spotifySavedTracksStat(r, spotifyKey),
		)
	}

	if bangumiKey != "" {
		stats = append(stats,
			s.bangumiAnimeCollectionsStat(r, bangumiKey),
			s.bangumiGameCollectionsStat(r, bangumiKey),
			s.bangumiAnimeWatchingStat(r, bangumiKey),
			s.bangumiAnimeWatchedStat(r, bangumiKey),
			s.bangumiAnimeWishStat(r, bangumiKey),
			s.bangumiGamePlayingStat(r, bangumiKey),
			s.bangumiGamePlayedStat(r, bangumiKey),
			s.bangumiGameWishStat(r, bangumiKey),
		)
	}

	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) githubFollowersStat(r *http.Request, username string) model.StatResponse {
	count, err := s.githubFollowersCached(r, username)
	if err != nil {
		return newErrStat("github", username, "followers", "GitHub Followers", "followers", err)
	}
	return newOKStat("github", username, "followers", "GitHub Followers", count, "followers")
}

func (s *Server) steamGamesStat(r *http.Request, key string) model.StatResponse {
	summary, err := s.steamSummaryCached(r, key)
	if err != nil {
		return newErrStat("steamgames", key, "games", "Steam Games", "games", err)
	}
	return newOKStat("steamgames", key, "games", "Steam Games", summary.GameCount, "games")
}

func (s *Server) steamPlaytimeStat(r *http.Request, key string) model.StatResponse {
	summary, err := s.steamSummaryCached(r, key)
	if err != nil {
		return newErrStat("steamtime", key, "playtime", "Steam Playtime", "hours", err)
	}
	hours := round1(float64(summary.TotalMinutes) / 60.0)
	return newOKStat("steamtime", key, "playtime", "Steam Playtime", hours, "hours")
}

func (s *Server) steamRecentPlaytimeStat(r *http.Request, key string) model.StatResponse {
	summary, err := s.steamSummaryCached(r, key)
	if err != nil {
		return newErrStat("steam2weekstime", key, "playtime_2weeks", "Steam Playtime (2 Weeks)", "hours", err)
	}
	recentHours := round1(float64(summary.RecentMinutes) / 60.0)
	return newOKStat("steam2weekstime", key, "playtime_2weeks", "Steam Playtime (2 Weeks)", recentHours, "hours")
}

func (s *Server) spotifyPlayingStat(r *http.Request, key string) model.StatResponse {
	status, err := s.spotifyStatusCached(r, key)
	if err != nil {
		return newErrStat("spotifyplaying", key, "status", "Spotify Now Playing", "track", err)
	}

	trackText := status.TrackName
	if len(status.Artists) > 0 {
		trackText = status.TrackName + " - " + strings.Join(status.Artists, ", ")
	}

	data := map[string]any{
		"isPlaying":  status.IsPlaying,
		"trackName":  status.TrackName,
		"artists":    status.Artists,
		"albumImage": status.AlbumImage,
		"progressMs": status.ProgressMS,
		"trackUrl":   status.TrackURL,
		"fromRecent": status.FromRecent,
	}
	if status.PlayedAt != "" {
		data["playedAt"] = status.PlayedAt
	}

	stat := newOKStat("spotifyplaying", key, "status", "Spotify Now Playing", trackText, "track")
	stat.Data = data
	return stat
}

func (s *Server) spotifySavedTracksStat(r *http.Request, key string) model.StatResponse {
	count, err := s.spotifySavedTracksCached(r, key)
	if err != nil {
		return newErrStat("spotifysaved", key, "saved_tracks", "Spotify Saved Tracks", "tracks", err)
	}
	return newOKStat("spotifysaved", key, "saved_tracks", "Spotify Saved Tracks", count, "tracks")
}

func (s *Server) bangumiAnimeCollectionsStat(r *http.Request, username string) model.StatResponse {
	return s.bangumiCollectionsStat(
		r,
		username,
		"bangumianime",
		"collections",
		"Bangumi Anime Collections",
		provider.BangumiSubjectAnime,
		provider.BangumiCollectionAll,
	)
}

func (s *Server) bangumiGameCollectionsStat(r *http.Request, username string) model.StatResponse {
	return s.bangumiCollectionsStat(
		r,
		username,
		"bangumigame",
		"collections",
		"Bangumi Game Collections",
		provider.BangumiSubjectGame,
		provider.BangumiCollectionAll,
	)
}

func (s *Server) bangumiAnimeWatchingStat(r *http.Request, username string) model.StatResponse {
	return s.bangumiCollectionsStat(
		r,
		username,
		"bangumianimewatching",
		"watching",
		"Bangumi Anime Watching",
		provider.BangumiSubjectAnime,
		provider.BangumiCollectionDo,
	)
}

func (s *Server) bangumiAnimeWatchedStat(r *http.Request, username string) model.StatResponse {
	return s.bangumiCollectionsStat(
		r,
		username,
		"bangumianimewatched",
		"watched",
		"Bangumi Anime Watched",
		provider.BangumiSubjectAnime,
		provider.BangumiCollectionCollect,
	)
}

func (s *Server) bangumiAnimeWishStat(r *http.Request, username string) model.StatResponse {
	return s.bangumiCollectionsStat(
		r,
		username,
		"bangumianimewish",
		"wish",
		"Bangumi Anime Wish",
		provider.BangumiSubjectAnime,
		provider.BangumiCollectionWish,
	)
}

func (s *Server) bangumiGamePlayingStat(r *http.Request, username string) model.StatResponse {
	return s.bangumiCollectionsStat(
		r,
		username,
		"bangumigameplaying",
		"playing",
		"Bangumi Game Playing",
		provider.BangumiSubjectGame,
		provider.BangumiCollectionDo,
	)
}

func (s *Server) bangumiGamePlayedStat(r *http.Request, username string) model.StatResponse {
	return s.bangumiCollectionsStat(
		r,
		username,
		"bangumigameplayed",
		"played",
		"Bangumi Game Played",
		provider.BangumiSubjectGame,
		provider.BangumiCollectionCollect,
	)
}

func (s *Server) bangumiGameWishStat(r *http.Request, username string) model.StatResponse {
	return s.bangumiCollectionsStat(
		r,
		username,
		"bangumigamewish",
		"wish",
		"Bangumi Game Wish",
		provider.BangumiSubjectGame,
		provider.BangumiCollectionWish,
	)
}

func (s *Server) bangumiCollectionsStat(
	r *http.Request,
	username string,
	source string,
	metric string,
	label string,
	subjectType int,
	collectionType int,
) model.StatResponse {
	summary, err := s.bangumiCollectionsCached(r, username, subjectType, collectionType)
	if err != nil {
		return newErrStat(source, username, metric, label, "items", err)
	}
	return newOKStat(source, username, metric, label, summary.Total, "items")
}

func (s *Server) githubFollowersCached(r *http.Request, username string) (int, error) {
	key := "github:" + strings.ToLower(strings.TrimSpace(username))
	if cached, ok := s.cache.Get(key); ok {
		if value, valid := cached.(int); valid {
			return value, nil
		}
	}

	value, err := s.github.Followers(r.Context(), username)
	if err != nil {
		return 0, err
	}
	s.cache.Set(key, value)
	return value, nil
}

func (s *Server) bangumiCollectionsCached(r *http.Request, username string, subjectType int, collectionType int) (provider.BangumiCollectionSummary, error) {
	key := "bangumi:" + strings.ToLower(strings.TrimSpace(username)) + ":" + strconv.Itoa(subjectType) + ":" + strconv.Itoa(collectionType)
	if cached, ok := s.cache.Get(key); ok {
		if value, valid := cached.(provider.BangumiCollectionSummary); valid {
			return value, nil
		}
	}

	if s.bangumi == nil {
		return provider.BangumiCollectionSummary{}, errors.New("bangumi client is not configured")
	}

	value, err := s.bangumi.CollectionsTotalByType(r.Context(), username, subjectType, collectionType)
	if err != nil {
		return provider.BangumiCollectionSummary{}, err
	}
	s.cache.Set(key, value)
	return value, nil
}

func (s *Server) steamSummaryCached(r *http.Request, idOrVanity string) (provider.OwnedGamesSummary, error) {
	key := "steam:" + strings.ToLower(strings.TrimSpace(idOrVanity))
	if cached, ok := s.cache.Get(key); ok {
		if value, valid := cached.(provider.OwnedGamesSummary); valid {
			return value, nil
		}
	}

	value, err := s.steam.OwnedGamesSummary(r.Context(), idOrVanity)
	if err != nil {
		return provider.OwnedGamesSummary{}, err
	}
	s.cache.Set(key, value)
	return value, nil
}

func (s *Server) spotifyStatusCached(r *http.Request, id string) (provider.SpotifyStatus, error) {
	key := "spotify:status:" + strings.ToLower(strings.TrimSpace(id))
	if cached, ok := s.cache.Get(key); ok {
		if value, valid := cached.(provider.SpotifyStatus); valid {
			return value, nil
		}
	}

	value, err := s.spotify.CurrentStatus(r.Context())
	if err != nil {
		return provider.SpotifyStatus{}, err
	}
	s.cache.Set(key, value)
	return value, nil
}

func (s *Server) spotifySavedTracksCached(r *http.Request, id string) (int, error) {
	key := "spotify:saved:" + strings.ToLower(strings.TrimSpace(id))
	if cached, ok := s.cache.Get(key); ok {
		if value, valid := cached.(int); valid {
			return value, nil
		}
	}

	value, err := s.spotify.SavedTracksCount(r.Context())
	if err != nil {
		return 0, err
	}
	s.cache.Set(key, value)
	return value, nil
}

func newOKStat(source, key, metric, label string, count any, unit string) model.StatResponse {
	return model.StatResponse{
		Source:    source,
		Key:       key,
		Metric:    metric,
		Label:     label,
		Failed:    false,
		Count:     count,
		Unit:      unit,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

func newErrStat(source, key, metric, label, unit string, err error) model.StatResponse {
	msg := "unknown error"
	if err != nil {
		msg = err.Error()
	}

	return model.StatResponse{
		Source:    source,
		Key:       key,
		Metric:    metric,
		Label:     label,
		Failed:    true,
		Count:     nil,
		Unit:      unit,
		UpdatedAt: time.Now().UTC().Format(time.RFC3339),
		Error:     &msg,
	}
}

func round1(v float64) float64 {
	return math.Round(v*10) / 10
}

func (s *Server) withCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := normalizeOrigin(r.Header.Get("Origin"))
		allowedOrigin := s.resolveCORSAllowedOrigin(origin)
		if allowedOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowedOrigin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			if allowedOrigin != "*" {
				w.Header().Add("Vary", "Origin")
			}
		}

		if r.Method == http.MethodOptions {
			if origin != "" && allowedOrigin == "" {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "cors origin is not allowed"})
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) resolveCORSAllowedOrigin(origin string) string {
	if origin == "" {
		return ""
	}
	if s.corsAllowAll {
		return "*"
	}
	if len(s.corsAllowedOrigins) == 0 {
		return ""
	}
	if _, ok := s.corsAllowedOrigins[origin]; ok {
		return origin
	}
	return ""
}

func normalizeCORSOrigins(origins []string) (bool, map[string]struct{}) {
	allowed := make(map[string]struct{}, len(origins))
	for _, origin := range origins {
		raw := strings.TrimSpace(origin)
		if raw == "" {
			continue
		}
		if raw == "*" {
			return true, map[string]struct{}{}
		}
		normalized := normalizeOrigin(raw)
		if normalized == "" {
			continue
		}
		allowed[normalized] = struct{}{}
	}
	return false, allowed
}

func normalizeOrigin(origin string) string {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return ""
	}
	if origin == "null" {
		return "null"
	}
	parsed, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	if parsed.User != nil {
		return ""
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	host := strings.ToLower(strings.TrimSpace(parsed.Host))
	if (scheme != "http" && scheme != "https") || host == "" {
		return ""
	}
	return scheme + "://" + host
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil && !errors.Is(err, http.ErrHandlerTimeout) {
		http.Error(w, `{"error":"failed to write response"}`, http.StatusInternalServerError)
	}
}
