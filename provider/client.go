package provider

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultBaseURL  = "https://api4.thetvdb.com/v4"
	defaultAPIKey   = "9bad61f9-16d5-468d-9c98-4c4038c13706"
	maxRetries      = 3
	maxResponseBody = 2 << 20 // 2 MB

	// episodesCacheTTL bounds how long a fetched bulk episode list is reused.
	// The server fetches one season at a time but the bulk endpoint returns the
	// whole series, so a short TTL lets every season of one series' refresh pass
	// share a single fetch while keeping the data fresh.
	episodesCacheTTL = 5 * time.Minute
	// seriesExtendedCacheTTL bounds how long an extended series record is reused.
	// The server requests one season gallery at a time, but every gallery needs
	// the same season index, so a short TTL lets one series refresh share a single
	// extended-series fetch while keeping the data fresh.
	seriesExtendedCacheTTL = 5 * time.Minute
	// maxEpisodePages caps pagination of the bulk episodes endpoint (500/page)
	// so a malformed paging response cannot spin forever.
	maxEpisodePages = 100
	// episodesCacheMaxEntries triggers an opportunistic purge of expired entries
	// so the cache cannot grow without bound during a long refresh run.
	episodesCacheMaxEntries       = 512
	seriesExtendedCacheMaxEntries = 512
)

// proxyPathPrefix is where the Silo metadata proxy mounts the TVDB v4 surface.
const proxyPathPrefix = "/v1/tvdb/4"

const (
	// proxyRetryJitter caps the random delay added after a proxy's Retry-After
	// so that clients told to wait the same time do not all retry at once.
	proxyRetryJitter = 250 * time.Millisecond
	// maxProxyRetryAfter caps a single Retry-After wait so that a malformed or
	// absurd header cannot park a request indefinitely.
	maxProxyRetryAfter = time.Hour
	// defaultProxyBackpressureBudget caps the total time one request spends
	// waiting on proxy backpressure, including when the caller set no deadline.
	defaultProxyBackpressureBudget = 10 * time.Minute
)

// Client is an HTTP client for the TVDB v4 API.
type Client struct {
	httpClient  *http.Client
	apiKey      string
	baseURL     string
	proxyMode   bool         // when true, baseURL is a Silo metadata proxy
	transportMu sync.RWMutex // protects baseURL and proxyMode
	token       string       // Bearer token from /login
	tokenMu     sync.RWMutex // protects token read/write
	refreshMu   sync.Mutex   // serialises re-auth attempts
	limiter     *rate.Limiter
	// proxyBackpressureBudget is the most one request waits on proxy
	// Retry-After responses in total.
	proxyBackpressureBudget time.Duration

	episodesCacheMu sync.Mutex
	episodesCache   map[string]episodesCacheEntry

	seriesExtendedCacheMu sync.Mutex
	seriesExtendedCache   map[int]seriesExtendedCacheEntry
}

// seriesEpisodes is the assembled (all-pages) result of the bulk episodes
// endpoint: the series record plus every episode for one season-type/language.
type seriesEpisodes struct {
	series   SeriesBaseRecord
	episodes []EpisodeBaseRecord
}

type episodesCacheEntry struct {
	data      *seriesEpisodes
	fetchedAt time.Time
}

type seriesExtendedCacheEntry struct {
	data      *SeriesExtendedRecord
	fetchedAt time.Time
}

// NewClient creates a TVDB API client with the given rate limit (requests per
// second). It uses the built-in project API key.
func NewClient(rateLimit int) *Client {
	if rateLimit <= 0 {
		rateLimit = 50
	}
	return &Client{
		httpClient:              &http.Client{Timeout: 30 * time.Second},
		apiKey:                  defaultAPIKey,
		baseURL:                 defaultBaseURL,
		limiter:                 rate.NewLimiter(rate.Limit(rateLimit), rateLimit),
		proxyBackpressureBudget: defaultProxyBackpressureBudget,
		episodesCache:           make(map[string]episodesCacheEntry),
		seriesExtendedCache:     make(map[int]seriesExtendedCacheEntry),
	}
}

// SetBaseURL overrides the API base URL. Used for testing.
func (c *Client) SetBaseURL(url string) {
	c.setTransport(url, false)
}

// SetProxyURL routes every request through a Silo metadata proxy at the given
// base URL (for example https://metadata.siloserver.org). The proxy answers
// /login itself and serves TVDB's own JSON, so the login flow and response
// handling are unchanged. An empty URL restores direct TVDB access. Safe to
// call while requests are in flight.
func (c *Client) SetProxyURL(proxyURL string) error {
	proxyURL = strings.TrimRight(strings.TrimSpace(proxyURL), "/")
	if proxyURL == "" {
		c.setTransport(defaultBaseURL, false)
		return nil
	}
	parsed, err := url.Parse(proxyURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("tvdb: invalid metadata proxy URL %q", proxyURL)
	}
	c.setTransport(proxyURL+proxyPathPrefix, true)
	return nil
}

// ProxyMode reports whether requests are routed through a Silo metadata proxy.
func (c *Client) ProxyMode() bool {
	c.transportMu.RLock()
	defer c.transportMu.RUnlock()
	return c.proxyMode
}

// transport returns the current base URL and whether it is a metadata proxy.
func (c *Client) transport() (string, bool) {
	c.transportMu.RLock()
	defer c.transportMu.RUnlock()
	return c.baseURL, c.proxyMode
}

// setTransport switches the upstream the client talks to. When the upstream
// changes, the bearer token and the in-memory memo caches are dropped so that
// nothing issued by, or fetched from, the previous upstream is reused. A
// request already in flight may still store its result afterwards; that is
// harmless because both upstreams serve the same TVDB data, and a stale token
// is replaced by the normal 401 refresh.
func (c *Client) setTransport(baseURL string, proxyMode bool) {
	c.transportMu.Lock()
	defer c.transportMu.Unlock()
	if c.baseURL == baseURL && c.proxyMode == proxyMode {
		return
	}
	c.baseURL = baseURL
	c.proxyMode = proxyMode

	c.tokenMu.Lock()
	c.token = ""
	c.tokenMu.Unlock()

	c.episodesCacheMu.Lock()
	clear(c.episodesCache)
	c.episodesCacheMu.Unlock()

	c.seriesExtendedCacheMu.Lock()
	clear(c.seriesExtendedCache)
	c.seriesExtendedCacheMu.Unlock()
}

// ---------------------------------------------------------------------------
// Authentication
// ---------------------------------------------------------------------------

// authenticate posts to /login with the API key and stores the bearer token.
func (c *Client) authenticate(ctx context.Context) error {
	body, err := json.Marshal(map[string]string{"apikey": c.apiKey})
	if err != nil {
		return fmt.Errorf("tvdb: marshal login body: %w", err)
	}
	baseURL, _ := c.transport()
	reqURL := baseURL + "/login"

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("tvdb: create login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("tvdb: login request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
		return fmt.Errorf("tvdb: login failed HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var loginResp loginResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(&loginResp); err != nil {
		return fmt.Errorf("tvdb: decode login response: %w", err)
	}
	if loginResp.Data.Token == "" {
		return fmt.Errorf("tvdb: login returned empty token")
	}

	c.tokenMu.Lock()
	c.token = loginResp.Data.Token
	c.tokenMu.Unlock()

	return nil
}

// ensureToken calls authenticate if no token is set.
func (c *Client) ensureToken(ctx context.Context) error {
	c.tokenMu.RLock()
	hasToken := c.token != ""
	c.tokenMu.RUnlock()

	if hasToken {
		return nil
	}
	return c.refreshToken(ctx, "")
}

// refreshToken performs a serialised re-authentication with a double-check
// pattern.
func (c *Client) refreshToken(ctx context.Context, oldToken string) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()

	c.tokenMu.RLock()
	current := c.token
	c.tokenMu.RUnlock()

	if current != oldToken {
		return nil // already refreshed
	}

	return c.authenticate(ctx)
}

// getToken returns the current bearer token.
func (c *Client) getToken() string {
	c.tokenMu.RLock()
	defer c.tokenMu.RUnlock()
	return c.token
}

// ---------------------------------------------------------------------------
// Core HTTP
// ---------------------------------------------------------------------------

// doGet executes a GET request against the TVDB API with rate limiting,
// Bearer token auth, automatic 401 refresh, and JSON decoding into dest.
func (c *Client) doGet(ctx context.Context, path string, dest any) error {
	authRetries := 0
	var backpressure time.Duration

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Read the transport and its token afresh on every attempt: switching
		// proxy settings clears the token, and a retry must go to the upstream
		// that the next login authenticates against.
		if err := c.ensureToken(ctx); err != nil {
			return err
		}
		baseURL, proxyMode := c.transport()
		reqURL := baseURL + path

		// Every HTTP attempt, retries included, goes through the rate limiter.
		if err := c.limiter.Wait(ctx); err != nil {
			return err
		}

		tok := c.getToken()

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
		if err != nil {
			return fmt.Errorf("tvdb: create request: %w", err)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("Authorization", "Bearer "+tok)

		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("tvdb: request failed: %w", err)
		}

		// 401 Unauthorized — refresh token and retry once.
		if resp.StatusCode == http.StatusUnauthorized {
			resp.Body.Close()
			authRetries++
			if authRetries > 1 {
				return fmt.Errorf("tvdb: authentication failed after token refresh")
			}
			if err := c.refreshToken(ctx, tok); err != nil {
				return fmt.Errorf("tvdb: token refresh failed: %w", err)
			}
			attempt--
			continue
		}

		// The Silo metadata proxy answers overload with 503 or 429 plus a
		// Retry-After. That is admission backpressure, not an upstream failure,
		// so wait as long as it asks, for as long as the caller's deadline
		// allows, instead of giving up after maxRetries. Without a usable
		// Retry-After the direct-mode handling below applies.
		if proxyMode && (resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests) {
			if delay, ok := retryAfterDelay(resp.Header.Get("Retry-After"), time.Now()); ok {
				_ = resp.Body.Close()
				wait := delay + time.Duration(rand.Int64N(int64(proxyRetryJitter)))
				if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) <= wait {
					return fmt.Errorf("tvdb: metadata proxy HTTP %d and Retry-After %s would exceed caller deadline", resp.StatusCode, delay)
				}
				if backpressure+wait > c.proxyBackpressureBudget {
					return fmt.Errorf("tvdb: metadata proxy HTTP %d still busy after waiting %s", resp.StatusCode, backpressure.Round(time.Second))
				}
				backpressure += wait
				slog.Debug("tvdb: metadata proxy busy, waiting for Retry-After",
					"path", path,
					"status", resp.StatusCode,
					"retry_after", delay.String(),
					"wait", wait.String(),
				)
				select {
				case <-time.After(wait):
				case <-ctx.Done():
					return ctx.Err()
				}
				attempt-- // backpressure waits do not use up maxRetries
				continue
			}
		}

		// 429 Too Many Requests.
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			retryAfter := resp.Header.Get("Retry-After")
			if attempt < maxRetries {
				backoff := retryAfterOrDefault(resp, attempt)
				slog.Warn("tvdb: rate limited by API, backing off",
					"path", path,
					"attempt", attempt+1,
					"retry_after_header", retryAfter,
					"backoff", backoff.String(),
				)
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}
			slog.Error("tvdb: rate limited after max retries",
				"path", path,
				"retries", maxRetries,
				"retry_after_header", retryAfter,
			)
			return fmt.Errorf("tvdb: rate limited after %d retries", maxRetries)
		}

		// 5xx — retry with exponential backoff.
		if resp.StatusCode >= 500 {
			resp.Body.Close()
			if attempt < maxRetries {
				backoff := time.Duration(1<<attempt) * time.Second
				select {
				case <-time.After(backoff):
				case <-ctx.Done():
					return ctx.Err()
				}
				continue
			}
			return fmt.Errorf("tvdb: server error %d after %d retries", resp.StatusCode, maxRetries)
		}

		// 4xx — client error, no retry.
		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
			resp.Body.Close()
			var apiErr apiError
			if err := json.Unmarshal(body, &apiErr); err == nil && apiErr.Message != "" {
				return fmt.Errorf("tvdb: HTTP %d: %s", resp.StatusCode, apiErr.Message)
			}
			return fmt.Errorf("tvdb: HTTP %d", resp.StatusCode)
		}

		// 2xx — decode response.
		decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBody)).Decode(dest)
		resp.Body.Close()
		if decodeErr != nil {
			return fmt.Errorf("tvdb: decode response: %w", decodeErr)
		}
		return nil
	}
	return fmt.Errorf("tvdb: max retries exceeded")
}

// retryAfterOrDefault parses the Retry-After header (seconds) or falls back
// to exponential backoff.
func retryAfterOrDefault(resp *http.Response, attempt int) time.Duration {
	if val := resp.Header.Get("Retry-After"); val != "" {
		if secs, err := strconv.Atoi(val); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return time.Duration(1<<attempt) * time.Second
}

// retryAfterDelay parses a Retry-After value given either in seconds or as an
// HTTP-date. It reports false when the value is absent, malformed, or does not
// ask for a positive wait. Waits are capped at maxProxyRetryAfter.
func retryAfterDelay(value string, now time.Time) (time.Duration, bool) {
	value = strings.TrimSpace(value)
	var delay time.Duration
	if secs, err := strconv.ParseInt(value, 10, 64); err == nil {
		// Clamp before converting so a huge value cannot overflow Duration.
		delay = time.Duration(min(secs, int64(maxProxyRetryAfter/time.Second))) * time.Second
	} else if at, err := http.ParseTime(value); err == nil {
		delay = at.Sub(now)
	}
	if delay <= 0 {
		return 0, false
	}
	return min(delay, maxProxyRetryAfter), true
}

// ---------------------------------------------------------------------------
// Public endpoint methods
// ---------------------------------------------------------------------------

// Search searches TVDB for entities matching the query.
func (c *Client) Search(ctx context.Context, query, mediaType string) ([]SearchResult, error) {
	path := "/search?query=" + url.QueryEscape(query)
	if mediaType != "" {
		path += "&type=" + url.QueryEscape(mediaType)
	}
	var resp apiResponse[[]SearchResult]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// SearchByRemoteID looks up TVDB entities by an external ID (e.g. IMDb ID).
func (c *Client) SearchByRemoteID(ctx context.Context, remoteID string) ([]RemoteIDResult, error) {
	path := "/search/remoteid/" + url.PathEscape(remoteID)
	var resp apiResponse[[]RemoteIDResult]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return resp.Data, nil
}

// GetPersonExtended fetches the extended record for a person.
func (c *Client) GetPersonExtended(ctx context.Context, id int) (*PeopleExtendedRecord, error) {
	path := fmt.Sprintf("/people/%d/extended?meta=translations", id)
	var resp apiResponse[PeopleExtendedRecord]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetSeriesExtended fetches the extended record for a series, including
// translations (needed to resolve preferred-language titles and overviews).
func (c *Client) GetSeriesExtended(ctx context.Context, id int) (*SeriesExtendedRecord, error) {
	if cached, ok := c.lookupSeriesExtendedCache(id); ok {
		return cached, nil
	}

	path := fmt.Sprintf("/series/%d/extended?meta=translations", id)
	var resp apiResponse[SeriesExtendedRecord]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	c.storeSeriesExtendedCache(id, &resp.Data)
	return &resp.Data, nil
}

func (c *Client) lookupSeriesExtendedCache(id int) (*SeriesExtendedRecord, bool) {
	c.seriesExtendedCacheMu.Lock()
	defer c.seriesExtendedCacheMu.Unlock()
	entry, ok := c.seriesExtendedCache[id]
	if !ok || time.Since(entry.fetchedAt) > seriesExtendedCacheTTL {
		return nil, false
	}
	return entry.data, true
}

func (c *Client) storeSeriesExtendedCache(id int, data *SeriesExtendedRecord) {
	c.seriesExtendedCacheMu.Lock()
	defer c.seriesExtendedCacheMu.Unlock()
	if len(c.seriesExtendedCache) >= seriesExtendedCacheMaxEntries {
		for cachedID, entry := range c.seriesExtendedCache {
			if time.Since(entry.fetchedAt) > seriesExtendedCacheTTL {
				delete(c.seriesExtendedCache, cachedID)
			}
		}
	}
	c.seriesExtendedCache[id] = seriesExtendedCacheEntry{data: data, fetchedAt: time.Now()}
}

// GetMovieExtended fetches the extended record for a movie, including
// translations (needed because movie records don't have an inline overview).
func (c *Client) GetMovieExtended(ctx context.Context, id int) (*MovieExtendedRecord, error) {
	path := fmt.Sprintf("/movies/%d/extended?meta=translations", id)
	var resp apiResponse[MovieExtendedRecord]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetSeasonExtended fetches the extended record for a season including
// episodes and translations.
func (c *Client) GetSeasonExtended(ctx context.Context, id int) (*SeasonExtendedRecord, error) {
	path := fmt.Sprintf("/seasons/%d/extended?meta=translations", id)
	var resp apiResponse[SeasonExtendedRecord]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetSeriesTranslation fetches a single-language translation for a series.
// lang3 must be a 3-letter ISO 639-2 code (e.g. "jpn", "fra").
func (c *Client) GetSeriesTranslation(ctx context.Context, id int, lang3 string) (*TranslationRecord, error) {
	path := fmt.Sprintf("/series/%d/translations/%s", id, url.PathEscape(lang3))
	var resp apiResponse[TranslationRecord]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetMovieTranslation fetches a single-language translation for a movie.
func (c *Client) GetMovieTranslation(ctx context.Context, id int, lang3 string) (*TranslationRecord, error) {
	path := fmt.Sprintf("/movies/%d/translations/%s", id, url.PathEscape(lang3))
	var resp apiResponse[TranslationRecord]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetSeasonTranslation fetches a single-language translation for a season.
func (c *Client) GetSeasonTranslation(ctx context.Context, id int, lang3 string) (*TranslationRecord, error) {
	path := fmt.Sprintf("/seasons/%d/translations/%s", id, url.PathEscape(lang3))
	var resp apiResponse[TranslationRecord]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// GetEpisodeTranslation fetches a single-language translation for an episode.
func (c *Client) GetEpisodeTranslation(ctx context.Context, id int, lang3 string) (*TranslationRecord, error) {
	path := fmt.Sprintf("/episodes/%d/translations/%s", id, url.PathEscape(lang3))
	var resp apiResponse[TranslationRecord]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, err
	}
	return &resp.Data, nil
}

// ---------------------------------------------------------------------------
// Bulk episodes (replaces the per-episode translation N+1)
// ---------------------------------------------------------------------------

// GetSeriesEpisodes fetches one page of a series' episodes for the given
// season-type. When lang3 is "" the base (series original-language) list is
// returned; when lang3 is a 3-letter ISO-639-2 code the translated list is
// returned. It returns the page's data plus pagination links (nil if absent).
func (c *Client) GetSeriesEpisodes(ctx context.Context, id int, seasonType, lang3 string, page int) (*SeriesEpisodesData, *links, error) {
	var path string
	if lang3 == "" {
		path = fmt.Sprintf("/series/%d/episodes/%s?page=%d", id, url.PathEscape(seasonType), page)
	} else {
		path = fmt.Sprintf("/series/%d/episodes/%s/%s?page=%d", id, url.PathEscape(seasonType), url.PathEscape(lang3), page)
	}
	var resp apiResponse[SeriesEpisodesData]
	if err := c.doGet(ctx, path, &resp); err != nil {
		return nil, nil, err
	}
	return &resp.Data, resp.Links, nil
}

// getAllSeriesEpisodes returns every episode for a series/season-type/language,
// assembling all pages of the bulk endpoint. Results are memoised for
// episodesCacheTTL keyed by (series, season-type, language) so that a series'
// per-season refresh pass shares a single fetch, and so a series with no
// translations is not re-fetched per season (negative caching).
func (c *Client) getAllSeriesEpisodes(ctx context.Context, id int, seasonType, lang3 string) (*seriesEpisodes, error) {
	key := fmt.Sprintf("%d|%s|%s", id, seasonType, lang3)
	if cached, ok := c.lookupEpisodesCache(key); ok {
		return cached, nil
	}

	var (
		series   SeriesBaseRecord
		episodes []EpisodeBaseRecord
	)
	for page := 0; page < maxEpisodePages; page++ {
		data, lnks, err := c.GetSeriesEpisodes(ctx, id, seasonType, lang3, page)
		if err != nil {
			return nil, err
		}
		if page == 0 {
			series = data.Series
		}
		episodes = append(episodes, data.Episodes...)
		if len(data.Episodes) == 0 || lnks == nil || lnks.Next == nil || *lnks.Next == "" {
			break
		}
	}

	result := &seriesEpisodes{series: series, episodes: episodes}
	c.storeEpisodesCache(key, result)
	return result, nil
}

// lookupEpisodesCache returns a cached entry if present and not expired.
func (c *Client) lookupEpisodesCache(key string) (*seriesEpisodes, bool) {
	c.episodesCacheMu.Lock()
	defer c.episodesCacheMu.Unlock()
	entry, ok := c.episodesCache[key]
	if !ok || time.Since(entry.fetchedAt) > episodesCacheTTL {
		return nil, false
	}
	return entry.data, true
}

// storeEpisodesCache records a result, opportunistically purging expired entries
// when the cache grows large.
func (c *Client) storeEpisodesCache(key string, data *seriesEpisodes) {
	c.episodesCacheMu.Lock()
	defer c.episodesCacheMu.Unlock()
	if len(c.episodesCache) >= episodesCacheMaxEntries {
		for k, e := range c.episodesCache {
			if time.Since(e.fetchedAt) > episodesCacheTTL {
				delete(c.episodesCache, k)
			}
		}
	}
	c.episodesCache[key] = episodesCacheEntry{data: data, fetchedAt: time.Now()}
}
