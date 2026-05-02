package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"zoumusictransfer/internal/model"
)

func TestExecuteTransferDryRunDoesNotCreateOrAdd(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "report.json")
	fake := &fakeSpotify{
		results: []model.Track{{
			Title:      "Song",
			TitleBrief: "Song",
			Artists:    []string{"Artist"},
			SpotifyURI: "spotify:track:123",
		}},
	}
	playlist := model.Playlist{
		ID:   "qq123",
		Name: "QQ List",
		Tracks: []model.Track{{
			Title:      "Song",
			TitleBrief: "Song",
			Artists:    []string{"Artist"},
		}},
	}

	report, err := executeTransfer(context.Background(), playlist, fake, "Target", true, model.MatchOptions{RequireLiveMatch: true}, reportPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if fake.created || fake.added {
		t.Fatal("dry run created playlist or added tracks")
	}
	if report.MatchedCount != 1 || report.FailedCount != 0 {
		t.Fatalf("report counts = matched %d failed %d, want 1/0", report.MatchedCount, report.FailedCount)
	}

	data, err := os.ReadFile(reportPath)
	if err != nil {
		t.Fatal(err)
	}
	var saved model.Report
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.MatchedCount != 1 || len(saved.Matched) != 1 {
		t.Fatalf("saved report matched count = %d len = %d, want 1/1", saved.MatchedCount, len(saved.Matched))
	}
}

func TestExecuteTransferReusesReportMatches(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "report.json")
	previous := model.Report{
		Playlist: "QQ List",
		Matched: []model.ReportMatch{{
			QQMID:          "qq-mid-1",
			QQTitle:        "Song",
			QQArtists:      []string{"Artist"},
			SpotifyTitle:   "Song",
			SpotifyArtists: []string{"Artist"},
			SpotifyURI:     "spotify:track:cached",
		}},
	}
	data, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	fake := &fakeSpotify{}
	playlist := model.Playlist{
		ID:   "qq123",
		Name: "QQ List",
		Tracks: []model.Track{{
			QQMID:      "qq-mid-1",
			Title:      "Song",
			TitleBrief: "Song",
			Artists:    []string{"Artist"},
		}},
	}

	report, err := executeTransfer(context.Background(), playlist, fake, "Target", true, model.MatchOptions{}, reportPath, true)
	if err != nil {
		t.Fatal(err)
	}
	if fake.searchCalls != 0 {
		t.Fatalf("search calls = %d, want 0", fake.searchCalls)
	}
	if report.MatchedCount != 1 || report.Matched[0].SpotifyURI != "spotify:track:cached" {
		t.Fatalf("report = %+v", report)
	}
}

func TestAddReportToSpotifyDoesNotSearch(t *testing.T) {
	reportPath := filepath.Join(t.TempDir(), "report.json")
	report := model.Report{
		Playlist: "QQ List",
		Matched: []model.ReportMatch{
			{QQTitle: "One", SpotifyURI: "spotify:track:one"},
			{QQTitle: "Duplicate", SpotifyURI: "spotify:track:one"},
			{QQTitle: "Two", SpotifyURI: "spotify:track:two"},
		},
	}
	data, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, data, 0600); err != nil {
		t.Fatal(err)
	}

	fake := &fakeSpotify{}
	updated, err := addReportToSpotify(context.Background(), fake, reportPath, "")
	if err != nil {
		t.Fatal(err)
	}
	if fake.searchCalls != 0 {
		t.Fatalf("search calls = %d, want 0", fake.searchCalls)
	}
	if !fake.created || !fake.added {
		t.Fatal("playlist was not created and added")
	}
	if fake.playlistName != "QQ List - QQ Import" {
		t.Fatalf("playlist name = %q", fake.playlistName)
	}
	if strings.Join(fake.addedURIs, ",") != "spotify:track:one,spotify:track:two" {
		t.Fatalf("added URIs = %v", fake.addedURIs)
	}
	if updated.SpotifyPlaylistID != "playlist123" {
		t.Fatalf("playlist ID = %q", updated.SpotifyPlaylistID)
	}
}

func TestLoadDotEnv(t *testing.T) {
	t.Setenv("DOTENV_EXISTING", "from-shell")
	dir := t.TempDir()
	path := filepath.Join(dir, ".env")
	content := strings.Join([]string{
		"# comment",
		"DOTENV_CLIENT_ID=abc123",
		"DOTENV_SECRET=\"quoted secret\"",
		"export DOTENV_REDIRECT_URL=http://127.0.0.1:8080/callback",
		"DOTENV_EXISTING=from-file",
	}, "\n")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}

	if err := loadDotEnv(path); err != nil {
		t.Fatal(err)
	}

	if got := os.Getenv("DOTENV_CLIENT_ID"); got != "abc123" {
		t.Fatalf("DOTENV_CLIENT_ID = %q", got)
	}
	if got := os.Getenv("DOTENV_SECRET"); got != "quoted secret" {
		t.Fatalf("DOTENV_SECRET = %q", got)
	}
	if got := os.Getenv("DOTENV_REDIRECT_URL"); got != "http://127.0.0.1:8080/callback" {
		t.Fatalf("DOTENV_REDIRECT_URL = %q", got)
	}
	if got := os.Getenv("DOTENV_EXISTING"); got != "from-shell" {
		t.Fatalf("DOTENV_EXISTING = %q, want shell value", got)
	}
}

type fakeSpotify struct {
	results      []model.Track
	searchCalls  int
	created      bool
	added        bool
	playlistName string
	addedURIs    []string
}

func (f *fakeSpotify) SearchTrack(context.Context, model.Track) ([]model.Track, error) {
	f.searchCalls++
	return f.results, nil
}

func (f *fakeSpotify) CurrentUserID(context.Context) (string, error) {
	return "user123", nil
}

func (f *fakeSpotify) CreatePlaylist(_ context.Context, _, name string) (string, error) {
	f.created = true
	f.playlistName = name
	return "playlist123", nil
}

func (f *fakeSpotify) AddTracksToPlaylist(_ context.Context, _ string, uris []string) error {
	f.added = true
	f.addedURIs = append([]string{}, uris...)
	return nil
}
