package qqmusic

import "testing"

func TestExtractPlaylistID(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "raw ID", input: "1234567890", want: "1234567890"},
		{name: "ryqq URL", input: "https://y.qq.com/n/ryqq/playlist/1234567890", want: "1234567890"},
		{name: "query ID", input: "https://y.qq.com/portal/playlist.html?id=1234567890", want: "1234567890"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ExtractPlaylistID(tt.input)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExtractPlaylistIDInvalid(t *testing.T) {
	if _, err := ExtractPlaylistID("https://example.com/nope"); err == nil {
		t.Fatal("expected error")
	}
}
