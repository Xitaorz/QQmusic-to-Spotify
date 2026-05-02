package qqmusic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"zoumusictransfer/internal/matcher"
	"zoumusictransfer/internal/model"
)

const playlistEndpoint = "https://c.y.qq.com/qzone/fcg-bin/fcg_ucc_getcdinfo_byids_cp.fcg"

type Client struct {
	HTTPClient *http.Client
	Cookie     string
}

func NewClient(cookie string) *Client {
	return &Client{
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Cookie:     cookie,
	}
}

func ExtractPlaylistID(input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", errors.New("empty QQ playlist input")
	}
	if regexp.MustCompile(`^\d+$`).MatchString(input) {
		return input, nil
	}
	parsed, err := url.Parse(input)
	if err != nil {
		return "", fmt.Errorf("parse QQ playlist URL: %w", err)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for i, part := range parts {
		if part == "playlist" && i+1 < len(parts) {
			id := strings.TrimSpace(parts[i+1])
			if regexp.MustCompile(`^\d+$`).MatchString(id) {
				return id, nil
			}
		}
	}
	if id := parsed.Query().Get("id"); regexp.MustCompile(`^\d+$`).MatchString(id) {
		return id, nil
	}
	return "", fmt.Errorf("could not extract QQ playlist ID from %q", input)
}

func (c *Client) FetchPlaylist(ctx context.Context, input string) (model.Playlist, error) {
	id, err := ExtractPlaylistID(input)
	if err != nil {
		return model.Playlist{}, err
	}
	values := url.Values{}
	values.Set("type", "1")
	values.Set("json", "1")
	values.Set("utf8", "1")
	values.Set("onlysong", "0")
	values.Set("disstid", id)
	values.Set("format", "json")
	values.Set("g_tk", "5381")
	values.Set("loginUin", "0")
	values.Set("hostUin", "0")
	values.Set("inCharset", "utf8")
	values.Set("outCharset", "utf-8")
	values.Set("notice", "0")
	values.Set("platform", "yqq")
	values.Set("needNewCode", "0")

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, playlistEndpoint+"?"+values.Encode(), nil)
	if err != nil {
		return model.Playlist{}, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 ZouMusicTransfer/1.0")
	req.Header.Set("Referer", "https://y.qq.com/")
	if c.Cookie != "" {
		req.Header.Set("Cookie", c.Cookie)
	}

	httpClient := c.HTTPClient
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return model.Playlist{}, fmt.Errorf("fetch QQ playlist: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return model.Playlist{}, fmt.Errorf("QQ playlist fetch returned HTTP %d", resp.StatusCode)
	}

	var payload playlistResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return model.Playlist{}, fmt.Errorf("decode QQ playlist response: %w", err)
	}
	if len(payload.CDList) == 0 {
		return model.Playlist{}, errors.New("QQ response did not include cdlist")
	}
	cd := payload.CDList[0]
	playlist := model.Playlist{
		ID:     id,
		Name:   strings.TrimSpace(cd.DissName),
		Tracks: make([]model.Track, 0, len(cd.SongList)),
	}
	for _, song := range cd.SongList {
		track := model.Track{
			Title:      strings.TrimSpace(song.SongName),
			TitleBrief: matcher.NormalizeTitleBrief(song.SongName),
			Album:      strings.TrimSpace(song.AlbumName),
			DurationMS: song.Interval * 1000,
			Live:       matcher.DetectLive(song.SongName),
			QQMID:      strings.TrimSpace(song.SongMID),
		}
		for _, singer := range song.Singers {
			name := strings.TrimSpace(singer.Name)
			if name != "" {
				track.Artists = append(track.Artists, name)
			}
		}
		playlist.Tracks = append(playlist.Tracks, track)
	}
	return playlist, nil
}

type playlistResponse struct {
	CDList []struct {
		DissName string   `json:"dissname"`
		SongList []qqSong `json:"songlist"`
	} `json:"cdlist"`
}

type qqSong struct {
	SongName  string `json:"songname"`
	SongMID   string `json:"songmid"`
	AlbumName string `json:"albumname"`
	Interval  int    `json:"interval"`
	Singers   []struct {
		Name string `json:"name"`
	} `json:"singer"`
}
