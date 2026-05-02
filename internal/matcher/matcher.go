package matcher

import (
	"math"

	"zoumusictransfer/internal/model"
)

func BestSpotifyMatch(qq model.Track, candidates []model.Track, opts model.MatchOptions) (model.Track, bool) {
	var best model.Track
	bestScore := math.MinInt
	for _, candidate := range candidates {
		score := Score(qq, candidate, opts)
		if score > bestScore {
			best = candidate
			bestScore = score
		}
	}
	return best, bestScore > 0
}

func Score(qq, spotify model.Track, opts model.MatchOptions) int {
	score := 100
	if NormalizeForCompare(qq.TitleBrief) != NormalizeForCompare(spotify.TitleBrief) {
		score -= 50
	}
	if len(qq.Artists) > 0 && len(spotify.Artists) > 0 &&
		NormalizeForCompare(qq.Artists[0]) != NormalizeForCompare(spotify.Artists[0]) {
		score -= 35
	}
	if opts.RequireAlbumMatch && NormalizeForCompare(qq.Album) != NormalizeForCompare(spotify.Album) {
		score -= 20
	}
	if opts.RequireLiveMatch && qq.Live != spotify.Live {
		score -= 30
	}
	if qq.DurationMS > 0 && spotify.DurationMS > 0 {
		diffSeconds := math.Abs(float64(qq.DurationMS-spotify.DurationMS)) / 1000
		switch {
		case diffSeconds > 30:
			score -= 20
		case diffSeconds > 10:
			score -= 10
		}
	}
	return score
}
