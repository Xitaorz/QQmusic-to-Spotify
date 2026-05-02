package spotify

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/oauth2"

	"zoumusictransfer/internal/matcher"
	"zoumusictransfer/internal/model"
)

const (
	defaultRedirectURL          = "http://localhost:8080/callback"
	defaultAPIBaseURL           = "https://api.spotify.com/v1"
	defaultMaxRetries           = 6
	defaultMaxRetryAfterSeconds = 120
	defaultMinIntervalMS        = 500
	defaultBatchAddIntervalMS   = 1000
)

type Client struct {
	HTTPClient       *http.Client
	BaseURL          string
	MinInterval      time.Duration
	BatchAddInterval time.Duration
	mu               sync.Mutex
	lastRequest      time.Time
}

type RateLimitError struct {
	Method     string
	Path       string
	RetryAfter time.Duration
	Body       string
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("Spotify rate limit exceeded; retry after %s", e.RetryAfter.Round(time.Second))
}

func Login(ctx context.Context) (*Client, error) {
	clientID := strings.TrimSpace(os.Getenv("SPOTIFY_CLIENT_ID"))
	clientSecret := strings.TrimSpace(os.Getenv("SPOTIFY_CLIENT_SECRET"))
	redirectURL := strings.TrimSpace(os.Getenv("SPOTIFY_REDIRECT_URL"))
	if redirectURL == "" {
		redirectURL = defaultRedirectURL
	}
	if clientID == "" || clientSecret == "" {
		return nil, errors.New("SPOTIFY_CLIENT_ID and SPOTIFY_CLIENT_SECRET are required")
	}

	config := oauth2.Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURL:  redirectURL,
		Scopes: []string{
			"playlist-read-private",
			"playlist-modify-public",
			"playlist-modify-private",
		},
		Endpoint: oauth2.Endpoint{
			AuthURL:  "https://accounts.spotify.com/authorize",
			TokenURL: "https://accounts.spotify.com/api/token",
		},
	}
	token, err := receiveToken(ctx, config, redirectURL)
	if err != nil {
		return nil, err
	}
	return &Client{
		HTTPClient:       config.Client(ctx, token),
		BaseURL:          defaultAPIBaseURL,
		MinInterval:      minIntervalFromEnv(),
		BatchAddInterval: batchAddIntervalFromEnv(),
	}, nil
}

func receiveToken(ctx context.Context, config oauth2.Config, redirectURL string) (*oauth2.Token, error) {
	parsed, err := url.Parse(redirectURL)
	if err != nil {
		return nil, fmt.Errorf("parse Spotify redirect URL: %w", err)
	}
	state, err := randomState()
	if err != nil {
		return nil, err
	}
	codeCh := make(chan string, 1)
	errCh := make(chan error, 1)
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:              parsed.Host,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	mux.HandleFunc(parsed.Path, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("state") != state {
			http.Error(w, "invalid state", http.StatusBadRequest)
			errCh <- errors.New("Spotify callback state mismatch")
			return
		}
		if errText := r.URL.Query().Get("error"); errText != "" {
			http.Error(w, "authorization failed", http.StatusBadRequest)
			errCh <- fmt.Errorf("Spotify authorization failed: %s", errText)
			return
		}
		code := r.URL.Query().Get("code")
		if code == "" {
			http.Error(w, "missing code", http.StatusBadRequest)
			errCh <- errors.New("Spotify callback missing code")
			return
		}
		_, _ = io.WriteString(w, "Spotify authorization received. You can close this window.")
		codeCh <- code
	})

	go func() {
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	fmt.Println("Open this Spotify authorization URL:")
	fmt.Println(config.AuthCodeURL(state, oauth2.AccessTypeOffline))

	select {
	case code := <-codeCh:
		return config.Exchange(ctx, code)
	case err := <-errCh:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Minute):
		return nil, errors.New("timed out waiting for Spotify authorization callback")
	}
}

func randomState() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate OAuth state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

func (c *Client) SearchTrack(ctx context.Context, track model.Track) ([]model.Track, error) {
	query := strings.TrimSpace(track.TitleBrief)
	if len(track.Artists) > 0 {
		query = strings.TrimSpace(query + " " + track.Artists[0])
	}
	values := url.Values{}
	values.Set("q", query)
	values.Set("type", "track")
	values.Set("limit", "10")

	var payload searchResponse
	if err := c.doJSON(ctx, http.MethodGet, "/search?"+values.Encode(), nil, &payload); err != nil {
		return nil, err
	}
	results := make([]model.Track, 0, len(payload.Tracks.Items))
	for _, item := range payload.Tracks.Items {
		result := model.Track{
			Title:      strings.TrimSpace(item.Name),
			TitleBrief: matcher.NormalizeTitleBrief(item.Name),
			Album:      strings.TrimSpace(item.Album.Name),
			DurationMS: item.DurationMS,
			Live:       matcher.DetectLive(item.Name),
			SpotifyID:  item.ID,
			SpotifyURI: item.URI,
		}
		for _, artist := range item.Artists {
			if name := strings.TrimSpace(artist.Name); name != "" {
				result.Artists = append(result.Artists, name)
			}
		}
		results = append(results, result)
	}
	return results, nil
}

func (c *Client) CurrentUserID(ctx context.Context) (string, error) {
	var payload struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, http.MethodGet, "/me", nil, &payload); err != nil {
		return "", err
	}
	if payload.ID == "" {
		return "", errors.New("Spotify /me response did not include id")
	}
	return payload.ID, nil
}

func (c *Client) CreatePlaylist(ctx context.Context, userID, name string) (string, error) {
	_ = userID
	body := map[string]any{
		"name":        name,
		"public":      false,
		"description": "Imported from QQ Music",
	}
	var payload struct {
		ID string `json:"id"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/me/playlists", body, &payload); err != nil {
		return "", err
	}
	if payload.ID == "" {
		return "", errors.New("Spotify create playlist response did not include id")
	}
	return payload.ID, nil
}

func (c *Client) AddTracksToPlaylist(ctx context.Context, playlistID string, uris []string) error {
	for start := 0; start < len(uris); start += 100 {
		end := start + 100
		if end > len(uris) {
			end = len(uris)
		}
		body := map[string]any{"uris": uris[start:end]}
		if err := c.doJSON(ctx, http.MethodPost, "/playlists/"+url.PathEscape(playlistID)+"/items", body, nil); err != nil {
			return err
		}
		if end < len(uris) {
			if err := sleepContext(ctx, c.batchAddInterval()); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *Client) doJSON(ctx context.Context, method, path string, body any, out any) error {
	var bodyData []byte
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		bodyData = data
	}
	base := c.BaseURL
	if base == "" {
		base = defaultAPIBaseURL
	}
	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	var lastBody string
	for attempt := 0; attempt <= defaultMaxRetries; attempt++ {
		var reader io.Reader
		if bodyData != nil {
			reader = bytes.NewReader(bodyData)
		}
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(base, "/")+path, reader)
		if err != nil {
			return err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		resp, err := c.do(ctx, httpClient, req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests {
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			lastBody = strings.TrimSpace(string(data))
			wait := retryDelay(resp.Header.Get("Retry-After"), attempt)
			resp.Body.Close()
			if wait > maxRetryAfter() {
				return &RateLimitError{
					Method:     method,
					Path:       path,
					RetryAfter: wait,
					Body:       lastBody,
				}
			}
			if attempt == defaultMaxRetries {
				break
			}
			fmt.Printf("Spotify rate limit hit; waiting %s before retrying %s\n", wait.Round(time.Second), path)
			if err := sleepContext(ctx, wait); err != nil {
				return err
			}
			continue
		}
		defer resp.Body.Close()
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			data, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
			return fmt.Errorf("Spotify API %s %s returned HTTP %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(data)))
		}
		if out == nil {
			return nil
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return fmt.Errorf("Spotify API %s %s returned HTTP 429 after retries: %s", method, path, lastBody)
}

func (c *Client) do(ctx context.Context, httpClient *http.Client, req *http.Request) (*http.Response, error) {
	if err := c.waitForTurn(ctx); err != nil {
		return nil, err
	}
	return httpClient.Do(req)
}

func (c *Client) waitForTurn(ctx context.Context) error {
	interval := c.minInterval()
	if interval <= 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	elapsed := time.Since(c.lastRequest)
	if !c.lastRequest.IsZero() && elapsed < interval {
		if err := sleepContext(ctx, interval-elapsed); err != nil {
			return err
		}
	}
	c.lastRequest = time.Now()
	return nil
}

func (c *Client) minInterval() time.Duration {
	if c.MinInterval < 0 {
		return 0
	}
	if c.MinInterval == 0 {
		return minIntervalFromEnv()
	}
	return c.MinInterval
}

func (c *Client) batchAddInterval() time.Duration {
	if c.BatchAddInterval < 0 {
		return 0
	}
	if c.BatchAddInterval == 0 {
		return batchAddIntervalFromEnv()
	}
	return c.BatchAddInterval
}

func maxRetryAfter() time.Duration {
	raw := strings.TrimSpace(os.Getenv("SPOTIFY_MAX_RETRY_SECONDS"))
	if raw == "" {
		return defaultMaxRetryAfterSeconds * time.Second
	}
	seconds, err := time.ParseDuration(raw + "s")
	if err != nil || seconds < 0 {
		return defaultMaxRetryAfterSeconds * time.Second
	}
	return seconds
}

func minIntervalFromEnv() time.Duration {
	return durationMSFromEnv("SPOTIFY_MIN_INTERVAL_MS", defaultMinIntervalMS)
}

func batchAddIntervalFromEnv() time.Duration {
	return durationMSFromEnv("SPOTIFY_BATCH_ADD_INTERVAL_MS", defaultBatchAddIntervalMS)
}

func durationMSFromEnv(key string, defaultMS int) time.Duration {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return time.Duration(defaultMS) * time.Millisecond
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms < 0 {
		return time.Duration(defaultMS) * time.Millisecond
	}
	return time.Duration(ms) * time.Millisecond
}

func retryDelay(retryAfter string, attempt int) time.Duration {
	if retryAfter != "" {
		if seconds, err := time.ParseDuration(strings.TrimSpace(retryAfter) + "s"); err == nil {
			return seconds
		}
		if when, err := http.ParseTime(retryAfter); err == nil {
			if delay := time.Until(when); delay > 0 {
				return delay
			}
			return 0
		}
	}
	delay := time.Duration(1<<attempt) * time.Second
	if delay > 30*time.Second {
		return 30 * time.Second
	}
	return delay
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

type searchResponse struct {
	Tracks struct {
		Items []struct {
			ID         string `json:"id"`
			URI        string `json:"uri"`
			Name       string `json:"name"`
			DurationMS int    `json:"duration_ms"`
			Artists    []struct {
				Name string `json:"name"`
			} `json:"artists"`
			Album struct {
				Name string `json:"name"`
			} `json:"album"`
		} `json:"items"`
	} `json:"tracks"`
}
