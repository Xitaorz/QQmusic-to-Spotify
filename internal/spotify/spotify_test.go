package spotify

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"zoumusictransfer/internal/model"
)

func TestAddTracksToPlaylistBatchesByHundred(t *testing.T) {
	var batchSizes []int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/playlists/playlist123/items" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		var payload struct {
			URIs []string `json:"uris"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		batchSizes = append(batchSizes, len(payload.URIs))
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	client := testClient(server)
	uris := make([]string, 101)
	for i := range uris {
		uris[i] = "spotify:track:test"
	}
	if err := client.AddTracksToPlaylist(context.Background(), "playlist123", uris); err != nil {
		t.Fatal(err)
	}
	if len(batchSizes) != 2 || batchSizes[0] != 100 || batchSizes[1] != 1 {
		t.Fatalf("batch sizes = %v, want [100 1]", batchSizes)
	}
}

func TestAddTracksToPlaylistZeroURIs(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	client := testClient(server)
	if err := client.AddTracksToPlaylist(context.Background(), "playlist123", nil); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("server was called for zero URIs")
	}
}

func TestAddTracksToPlaylistOneAndHundred(t *testing.T) {
	for _, count := range []int{1, 100} {
		t.Run("count", func(t *testing.T) {
			calls := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls++
				var payload struct {
					URIs []string `json:"uris"`
				}
				if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
					t.Fatal(err)
				}
				if len(payload.URIs) != count {
					t.Fatalf("batch size = %d, want %d", len(payload.URIs), count)
				}
				w.WriteHeader(http.StatusCreated)
			}))
			client := testClient(server)
			uris := make([]string, count)
			for i := range uris {
				uris[i] = "spotify:track:test"
			}
			if err := client.AddTracksToPlaylist(context.Background(), "playlist123", uris); err != nil {
				t.Fatal(err)
			}
			server.Close()
			if calls != 1 {
				t.Fatalf("calls = %d, want 1", calls)
			}
		})
	}
}

func TestCreatePlaylistUsesCurrentUserEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/me/playlists" {
			t.Fatalf("path = %s, want /me/playlists", r.URL.Path)
		}
		var payload struct {
			Name   string `json:"name"`
			Public bool   `json:"public"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload.Name != "Imported" || payload.Public {
			t.Fatalf("payload = %+v", payload)
		}
		_, _ = w.Write([]byte(`{"id":"playlist123"}`))
	}))
	defer server.Close()

	client := testClient(server)
	id, err := client.CreatePlaylist(context.Background(), "ignored-user-id", "Imported")
	if err != nil {
		t.Fatal(err)
	}
	if id != "playlist123" {
		t.Fatalf("id = %q, want playlist123", id)
	}
}

func TestSearchTrackRetriesAfterRateLimit(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls == 1 {
			w.Header().Set("Retry-After", "0")
			http.Error(w, "Too many requests", http.StatusTooManyRequests)
			return
		}
		if r.URL.Path != "/search" {
			t.Fatalf("path = %s, want /search", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "Song Artist" {
			t.Fatalf("query = %q, want Song Artist", got)
		}
		_, _ = w.Write([]byte(`{
			"tracks": {
				"items": [{
					"id": "id123",
					"uri": "spotify:track:id123",
					"name": "Song",
					"duration_ms": 180000,
					"artists": [{"name": "Artist"}],
					"album": {"name": "Album"}
				}]
			}
		}`))
	}))
	defer server.Close()

	client := testClient(server)
	results, err := client.SearchTrack(context.Background(), model.Track{
		TitleBrief: "Song",
		Artists:    []string{"Artist"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
	if len(results) != 1 || results[0].SpotifyURI != "spotify:track:id123" {
		t.Fatalf("results = %+v", results)
	}
}

func TestSearchTrackReturnsAfterRepeatedRateLimit(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Retry-After", "0")
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := testClient(server)
	_, err := client.SearchTrack(context.Background(), model.Track{TitleBrief: "Song"})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	if calls != defaultMaxRetries+1 {
		t.Fatalf("calls = %d, want %d", calls, defaultMaxRetries+1)
	}
}

func TestRetryDelayParsesHTTPDate(t *testing.T) {
	when := time.Now().Add(2 * time.Second).UTC().Format(http.TimeFormat)
	delay := retryDelay(when, 0)
	if delay <= 0 {
		t.Fatalf("delay = %s, want positive", delay)
	}
}

func TestSearchTrackReturnsLongRateLimit(t *testing.T) {
	t.Setenv("SPOTIFY_MAX_RETRY_SECONDS", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "86400")
		http.Error(w, "Too many requests", http.StatusTooManyRequests)
	}))
	defer server.Close()

	client := testClient(server)
	_, err := client.SearchTrack(context.Background(), model.Track{TitleBrief: "Song"})
	if err == nil {
		t.Fatal("expected rate limit error")
	}
	var rateLimitErr *RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("error = %T, want RateLimitError", err)
	}
	if rateLimitErr.RetryAfter != 24*time.Hour {
		t.Fatalf("retry after = %s, want 24h", rateLimitErr.RetryAfter)
	}
}

func TestClientAppliesMinInterval(t *testing.T) {
	var requestTimes []time.Time
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestTimes = append(requestTimes, time.Now())
		_, _ = w.Write([]byte(`{"id":"user123"}`))
	}))
	defer server.Close()

	client := &Client{
		HTTPClient:       server.Client(),
		BaseURL:          server.URL,
		MinInterval:      25 * time.Millisecond,
		BatchAddInterval: -1,
	}
	if _, err := client.CurrentUserID(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := client.CurrentUserID(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(requestTimes) != 2 {
		t.Fatalf("request count = %d, want 2", len(requestTimes))
	}
	if gap := requestTimes[1].Sub(requestTimes[0]); gap < 20*time.Millisecond {
		t.Fatalf("request gap = %s, want at least about 25ms", gap)
	}
}

func TestDurationMSFromEnv(t *testing.T) {
	t.Setenv("SPOTIFY_MIN_INTERVAL_MS", "750")
	if got := minIntervalFromEnv(); got != 750*time.Millisecond {
		t.Fatalf("min interval = %s, want 750ms", got)
	}
}

func testClient(server *httptest.Server) *Client {
	return &Client{
		HTTPClient:       server.Client(),
		BaseURL:          server.URL,
		MinInterval:      -1,
		BatchAddInterval: -1,
	}
}
