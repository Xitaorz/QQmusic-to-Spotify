package matcher

import "testing"

func TestNormalizeTitleBrief(t *testing.T) {
	tests := map[string]string{
		"Song (Live)":         "Song",
		"Song（现场版）":           "Song",
		"Song - Remix":        "Song",
		"  Song   Name  ":     "Song Name",
		"Song (Original Mix)": "Song",
	}
	for input, want := range tests {
		if got := NormalizeTitleBrief(input); got != want {
			t.Fatalf("NormalizeTitleBrief(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestDetectLive(t *testing.T) {
	for _, input := range []string{"Song Live", "歌曲 现场版", "Concert Cut"} {
		if !DetectLive(input) {
			t.Fatalf("DetectLive(%q) = false, want true", input)
		}
	}
	if DetectLive("Studio Version") {
		t.Fatal("DetectLive studio track = true, want false")
	}
}
