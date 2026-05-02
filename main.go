package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"zoumusictransfer/internal/matcher"
	"zoumusictransfer/internal/model"
	"zoumusictransfer/internal/qqmusic"
	"zoumusictransfer/internal/spotify"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run() error {
	if err := loadDotEnv(".env"); err != nil {
		return err
	}

	var (
		qqInput       = flag.String("qq-playlist", "", "QQ Music playlist URL or raw playlist ID")
		spotifyName   = flag.String("spotify-name", "", "target Spotify playlist name override")
		dryRun        = flag.Bool("dry-run", false, "search and match only; do not create or add tracks")
		requireAlbum  = flag.Bool("require-album", false, "require album match during Spotify candidate scoring")
		ignoreLive    = flag.Bool("ignore-live", false, "disable live-version matching")
		qqCookie      = flag.String("qq-cookie", "", "optional QQ Music cookie for private playlists")
		reportPath    = flag.String("report", "transfer_report.json", "path to write JSON transfer report")
		fresh         = flag.Bool("fresh", false, "ignore existing report matches and search every track again")
		addFromReport = flag.Bool("add-from-report", false, "create a Spotify playlist from existing report matches without QQ fetch or Spotify search")
	)
	flag.Parse()
	if *qqInput == "" && !*addFromReport {
		return fmt.Errorf("--qq-playlist is required")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()

	if *addFromReport {
		spClient, err := spotify.Login(ctx)
		if err != nil {
			return err
		}
		_, err = addReportToSpotify(ctx, spClient, *reportPath, *spotifyName)
		return err
	}

	qqClient := qqmusic.NewClient(*qqCookie)
	qqPlaylist, err := qqClient.FetchPlaylist(ctx, *qqInput)
	if err != nil {
		return err
	}
	targetName := *spotifyName
	if targetName == "" {
		targetName = qqPlaylist.Name + " - QQ Import"
	}

	spClient, err := spotify.Login(ctx)
	if err != nil {
		return err
	}

	options := model.MatchOptions{
		RequireAlbumMatch: *requireAlbum,
		RequireLiveMatch:  !*ignoreLive,
	}
	_, err = executeTransfer(ctx, qqPlaylist, spClient, targetName, *dryRun, options, *reportPath, !*fresh)
	return err
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("load %s: %w", path, err)
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	lineNumber := 0
	for scanner.Scan() {
		lineNumber++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimPrefix(line, "export ")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			return fmt.Errorf("%s:%d: expected KEY=value", path, lineNumber)
		}
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" {
			return fmt.Errorf("%s:%d: empty environment key", path, lineNumber)
		}
		if len(value) >= 2 {
			if (value[0] == '"' && value[len(value)-1] == '"') || (value[0] == '\'' && value[len(value)-1] == '\'') {
				value = value[1 : len(value)-1]
			}
		}
		if _, exists := os.LookupEnv(key); !exists {
			if err := os.Setenv(key, value); err != nil {
				return fmt.Errorf("%s:%d: set %s: %w", path, lineNumber, key, err)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	return nil
}

type spotifyService interface {
	SearchTrack(context.Context, model.Track) ([]model.Track, error)
	CurrentUserID(context.Context) (string, error)
	CreatePlaylist(context.Context, string, string) (string, error)
	AddTracksToPlaylist(context.Context, string, []string) error
}

func addReportToSpotify(ctx context.Context, spClient spotifyService, reportPath, playlistNameOverride string) (model.Report, error) {
	report, err := readReport(reportPath)
	if err != nil {
		return report, err
	}
	if len(report.Matched) == 0 {
		return report, fmt.Errorf("report %s has no matched Spotify tracks to add", reportPath)
	}

	targetName := playlistNameOverride
	if targetName == "" {
		targetName = report.Playlist + " - QQ Import"
	}

	uris := make([]string, 0, len(report.Matched))
	seen := map[string]bool{}
	for _, match := range report.Matched {
		if match.SpotifyURI == "" || seen[match.SpotifyURI] {
			continue
		}
		seen[match.SpotifyURI] = true
		uris = append(uris, match.SpotifyURI)
	}
	if len(uris) == 0 {
		return report, fmt.Errorf("report %s has no usable Spotify URIs", reportPath)
	}

	userID, err := spClient.CurrentUserID(ctx)
	if err != nil {
		return report, err
	}
	playlistID, err := spClient.CreatePlaylist(ctx, userID, targetName)
	if err != nil {
		return report, err
	}
	if err := spClient.AddTracksToPlaylist(ctx, playlistID, uris); err != nil {
		return report, err
	}

	report.SpotifyPlaylistID = playlistID
	report.MatchedCount = len(report.Matched)
	report.FailedCount = len(report.Failed)
	report.DryRun = false
	if err := writeReport(reportPath, report); err != nil {
		return report, err
	}
	fmt.Printf("Added %d saved Spotify tracks from %s\n", len(uris), reportPath)
	printSummary(targetName, report, reportPath)
	return report, nil
}

func executeTransfer(ctx context.Context, qqPlaylist model.Playlist, spClient spotifyService, targetName string, dryRun bool, options model.MatchOptions, reportPath string, resume bool) (model.Report, error) {
	report := model.Report{
		Playlist:    qqPlaylist.Name,
		TotalTracks: len(qqPlaylist.Tracks),
		DryRun:      dryRun,
		Matched:     []model.ReportMatch{},
		Failed:      []model.ReportFail{},
	}
	var matchedURIs []string
	resumeMatches, err := loadResumeMatches(reportPath, resume)
	if err != nil {
		return report, err
	}

	for i, qqTrack := range qqPlaylist.Tracks {
		if existing, ok := resumeMatches[trackKey(qqTrack.QQMID, qqTrack.Title, qqTrack.Artists)]; ok {
			fmt.Printf("[%d/%d] Reusing previous match: %s\n", i+1, len(qqPlaylist.Tracks), qqTrack.Title)
			matchedURIs = append(matchedURIs, existing.SpotifyURI)
			report.Matched = append(report.Matched, existing)
			continue
		}
		fmt.Printf("[%d/%d] Searching: %s", i+1, len(qqPlaylist.Tracks), qqTrack.Title)
		if len(qqTrack.Artists) > 0 {
			fmt.Printf(" - %s", qqTrack.Artists[0])
		}
		fmt.Println()
		candidates, err := spClient.SearchTrack(ctx, qqTrack)
		if err != nil {
			var rateLimitErr *spotify.RateLimitError
			report.Failed = append(report.Failed, model.ReportFail{
				QQTitle:   qqTrack.Title,
				QQArtists: qqTrack.Artists,
				Reason:    "spotify search failed: " + err.Error(),
			})
			if errors.As(err, &rateLimitErr) {
				report.MatchedCount = len(report.Matched)
				report.FailedCount = len(report.Failed)
				if writeErr := writeReport(reportPath, report); writeErr != nil {
					return report, writeErr
				}
				printSummary(targetName, report, reportPath)
				return report, fmt.Errorf("Spotify asked us to wait %s before more requests, so the run stopped early; try again later or increase SPOTIFY_MAX_RETRY_SECONDS", rateLimitErr.RetryAfter.Round(time.Second))
			}
			continue
		}
		best, ok := matcher.BestSpotifyMatch(qqTrack, candidates, options)
		if !ok {
			report.Failed = append(report.Failed, model.ReportFail{
				QQTitle:   qqTrack.Title,
				QQArtists: qqTrack.Artists,
				Reason:    "no confident Spotify match",
			})
			continue
		}
		matchedURIs = append(matchedURIs, best.SpotifyURI)
		report.Matched = append(report.Matched, model.ReportMatch{
			QQMID:          qqTrack.QQMID,
			QQTitle:        qqTrack.Title,
			QQArtists:      qqTrack.Artists,
			SpotifyTitle:   best.Title,
			SpotifyArtists: best.Artists,
			SpotifyURI:     best.SpotifyURI,
		})
	}

	report.MatchedCount = len(report.Matched)
	report.FailedCount = len(report.Failed)

	if !dryRun {
		userID, err := spClient.CurrentUserID(ctx)
		if err != nil {
			return report, err
		}
		playlistID, err := spClient.CreatePlaylist(ctx, userID, targetName)
		if err != nil {
			return report, err
		}
		report.SpotifyPlaylistID = playlistID
		if err := spClient.AddTracksToPlaylist(ctx, playlistID, matchedURIs); err != nil {
			return report, err
		}
	}

	if err := writeReport(reportPath, report); err != nil {
		return report, err
	}
	printSummary(targetName, report, reportPath)
	return report, nil
}

func loadResumeMatches(reportPath string, resume bool) (map[string]model.ReportMatch, error) {
	matches := map[string]model.ReportMatch{}
	if !resume {
		return matches, nil
	}
	file, err := os.Open(reportPath)
	if err != nil {
		if os.IsNotExist(err) {
			return matches, nil
		}
		return nil, err
	}
	defer file.Close()

	previous, err := decodeReport(file)
	if err != nil {
		return nil, fmt.Errorf("read resume report %s: %w", reportPath, err)
	}
	for _, match := range previous.Matched {
		if match.SpotifyURI == "" {
			continue
		}
		matches[trackKey(match.QQMID, match.QQTitle, match.QQArtists)] = match
	}
	if len(matches) > 0 {
		fmt.Printf("Loaded %d previous matches from %s\n", len(matches), reportPath)
	}
	return matches, nil
}

func readReport(reportPath string) (model.Report, error) {
	file, err := os.Open(reportPath)
	if err != nil {
		return model.Report{}, err
	}
	defer file.Close()
	report, err := decodeReport(file)
	if err != nil {
		return report, fmt.Errorf("read report %s: %w", reportPath, err)
	}
	return report, nil
}

func decodeReport(file *os.File) (model.Report, error) {
	var report model.Report
	if err := json.NewDecoder(file).Decode(&report); err != nil {
		return report, err
	}
	return report, nil
}

func trackKey(qqMID, title string, artists []string) string {
	if strings.TrimSpace(qqMID) != "" {
		return "mid:" + strings.TrimSpace(qqMID)
	}
	parts := []string{strings.ToLower(strings.TrimSpace(title))}
	for _, artist := range artists {
		if trimmed := strings.ToLower(strings.TrimSpace(artist)); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return "meta:" + strings.Join(parts, "\x00")
}

func writeReport(path string, report model.Report) error {
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(report)
}

func printSummary(targetName string, report model.Report, reportPath string) {
	fmt.Println()
	fmt.Println("Imported playlist name:", report.Playlist)
	fmt.Println("Target Spotify playlist name:", targetName)
	fmt.Println("Total tracks:", report.TotalTracks)
	fmt.Println("Matched tracks:", report.MatchedCount)
	fmt.Println("Failed tracks:", report.FailedCount)
	fmt.Println("Dry run:", report.DryRun)
	if report.SpotifyPlaylistID != "" {
		fmt.Println("Created Spotify playlist ID:", report.SpotifyPlaylistID)
	}
	fmt.Println("Report:", reportPath)
	if len(report.Failed) > 0 {
		fmt.Println()
		fmt.Println("Failed tracks:")
		for _, failed := range report.Failed {
			fmt.Printf("- %s: %s\n", failed.QQTitle, failed.Reason)
		}
	}
}
