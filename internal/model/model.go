package model

type Track struct {
	Title      string   `json:"title"`
	TitleBrief string   `json:"title_brief"`
	Artists    []string `json:"artists"`
	Album      string   `json:"album"`
	DurationMS int      `json:"duration_ms"`
	Live       bool     `json:"live"`

	QQMID      string `json:"qq_mid,omitempty"`
	SpotifyID  string `json:"spotify_id,omitempty"`
	SpotifyURI string `json:"spotify_uri,omitempty"`
}

type Playlist struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Tracks []Track `json:"tracks"`
}

type MatchOptions struct {
	RequireAlbumMatch bool
	RequireLiveMatch  bool
}

type Report struct {
	Playlist          string        `json:"playlist"`
	TotalTracks       int           `json:"total_tracks"`
	MatchedCount      int           `json:"matched_count"`
	FailedCount       int           `json:"failed_count"`
	DryRun            bool          `json:"dry_run"`
	SpotifyPlaylistID string        `json:"spotify_playlist_id,omitempty"`
	Matched           []ReportMatch `json:"matched"`
	Failed            []ReportFail  `json:"failed"`
}

type ReportMatch struct {
	QQMID          string   `json:"qq_mid,omitempty"`
	QQTitle        string   `json:"qq_title"`
	QQArtists      []string `json:"qq_artists"`
	SpotifyTitle   string   `json:"spotify_title"`
	SpotifyArtists []string `json:"spotify_artists"`
	SpotifyURI     string   `json:"spotify_uri"`
}

type ReportFail struct {
	QQTitle   string   `json:"qq_title"`
	QQArtists []string `json:"qq_artists"`
	Reason    string   `json:"reason"`
}
