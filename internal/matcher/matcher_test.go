package matcher

import (
	"testing"

	"zoumusictransfer/internal/model"
)

func TestScorePenalties(t *testing.T) {
	qq := model.Track{TitleBrief: "Song", Artists: []string{"Artist"}, Album: "Album", DurationMS: 180000}
	sp := model.Track{TitleBrief: "Other", Artists: []string{"Other"}, Album: "Different", DurationMS: 225000}
	score := Score(qq, sp, model.MatchOptions{RequireAlbumMatch: true, RequireLiveMatch: true})
	if score != -25 {
		t.Fatalf("score = %d, want -25", score)
	}
}

func TestBestSpotifyMatch(t *testing.T) {
	qq := model.Track{TitleBrief: "Song", Artists: []string{"Artist"}, DurationMS: 180000}
	candidates := []model.Track{
		{TitleBrief: "Other", Artists: []string{"Artist"}, SpotifyURI: "bad"},
		{TitleBrief: "Song", Artists: []string{"Artist"}, SpotifyURI: "good"},
	}
	best, ok := BestSpotifyMatch(qq, candidates, model.MatchOptions{RequireLiveMatch: true})
	if !ok {
		t.Fatal("expected match")
	}
	if best.SpotifyURI != "good" {
		t.Fatalf("best URI = %q, want good", best.SpotifyURI)
	}
}

func TestRequireLiveMatchPenalty(t *testing.T) {
	qq := model.Track{TitleBrief: "Song", Artists: []string{"Artist"}, Live: true}
	sp := model.Track{TitleBrief: "Song", Artists: []string{"Artist"}, Live: false}
	withLive := Score(qq, sp, model.MatchOptions{RequireLiveMatch: true})
	withoutLive := Score(qq, sp, model.MatchOptions{RequireLiveMatch: false})
	if withoutLive-withLive != 30 {
		t.Fatalf("live penalty diff = %d, want 30", withoutLive-withLive)
	}
}
