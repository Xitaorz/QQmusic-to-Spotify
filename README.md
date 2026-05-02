# QQ Music to Spotify Transfer

A Go CLI for importing a QQ Music playlist into Spotify by metadata matching.

The tool does not convert QQ Music IDs into Spotify IDs. Instead, it fetches QQ playlist metadata, searches Spotify for each track, picks the best match, and then creates a Spotify playlist from the matched Spotify track URIs.

## Features

- Accepts a QQ Music playlist URL or raw playlist ID.
- Fetches public QQ Music playlist tracks.
- Normalizes titles and detects live versions.
- Uses Spotify OAuth Authorization Code Flow.
- Searches Spotify with throttling to reduce rate-limit hits.
- Matches tracks by title, artist, optional album, live marker, and duration.
- Supports dry runs before changing Spotify.
- Saves a JSON report for review, resume, and later importing.
- Can add already matched report tracks without searching Spotify again.

## Requirements

- Go 1.24+
- A Spotify account
- A Spotify Developer app

## Spotify App Setup

Create an app at:

https://developer.spotify.com/dashboard

Add this redirect URI exactly:

```text
http://127.0.0.1:8080/callback
```

If your Spotify app is in development mode, add the Spotify account you will authorize with under the app's user/access management page.

Required Spotify scopes:

```text
playlist-read-private
playlist-modify-public
playlist-modify-private
```

## Configuration

Copy the example env file:

```powershell
Copy-Item ".env.example" ".env"
```

Edit `.env`:

```env
SPOTIFY_CLIENT_ID=your_client_id_here
SPOTIFY_CLIENT_SECRET=your_client_secret_here
SPOTIFY_REDIRECT_URL=http://127.0.0.1:8080/callback
SPOTIFY_MIN_INTERVAL_MS=500
SPOTIFY_BATCH_ADD_INTERVAL_MS=1000
SPOTIFY_MAX_RETRY_SECONDS=120
```

Do not commit `.env`. It contains your Spotify client secret.

## Usage

Run a dry run first:

```powershell
go run . --qq-playlist "https://y.qq.com/n/ryqq/playlist/1234567890" --dry-run
```

This will fetch QQ tracks, search Spotify, match tracks, and write:

```text
transfer_report.json
```

It will not create a playlist or add tracks.

To create a Spotify playlist and add matched tracks:

```powershell
go run . --qq-playlist "https://y.qq.com/n/ryqq/playlist/1234567890" --spotify-name "Imported QQ Songs"
```

To add only the tracks already saved in `transfer_report.json`, without fetching QQ or searching Spotify:

```powershell
go run . --add-from-report --report transfer_report.json --spotify-name "Imported QQ Songs"
```

## CLI Flags

```text
--qq-playlist        QQ Music playlist URL or raw playlist ID
--spotify-name       target Spotify playlist name override
--dry-run            search and match only; do not create or add tracks
--require-album      require album match during matching
--ignore-live        disable live-version matching
--qq-cookie          optional QQ Music cookie for private playlists
--report             report path, default transfer_report.json
--fresh              ignore previous report matches and search everything again
--add-from-report    add saved report matches to Spotify without searching
```

## Resume Behavior

By default, the tool reuses successful matches from the report file.

If `transfer_report.json` already contains a matched Spotify URI for a QQ track, the next run skips Spotify search for that track and reuses the saved URI.

Use `--fresh` to force a new search:

```powershell
go run . --qq-playlist "https://y.qq.com/n/ryqq/playlist/1234567890" --dry-run --fresh
```

## Rate Limits

Spotify does not publish one fixed request-per-second quota. This tool uses a global Spotify request limiter:

- `SPOTIFY_MIN_INTERVAL_MS=500` waits at least 500ms between Spotify API calls.
- `SPOTIFY_BATCH_ADD_INTERVAL_MS=1000` waits between add-track batches.
- `SPOTIFY_MAX_RETRY_SECONDS=120` stops early if Spotify asks the tool to wait longer than 120 seconds.

If you hit rate limits, use a slower setting:

```env
SPOTIFY_MIN_INTERVAL_MS=1000
SPOTIFY_BATCH_ADD_INTERVAL_MS=1500
SPOTIFY_MAX_RETRY_SECONDS=120
```

When Spotify returns HTTP 429, the tool reads the `Retry-After` header and retries when the wait is short enough.

## Report Format

The report contains matched and failed tracks:

```json
{
  "playlist": "QQ Playlist Name",
  "matched": [
    {
      "qq_title": "Song",
      "qq_artists": ["Artist"],
      "spotify_title": "Song",
      "spotify_artists": ["Artist"],
      "spotify_uri": "spotify:track:..."
    }
  ],
  "failed": [
    {
      "qq_title": "Missing Song",
      "qq_artists": ["Artist"],
      "reason": "no confident Spotify match"
    }
  ]
}
```

You can inspect this file before doing a real import.

## Testing

```powershell
go test ./...
```

## Notes

- Public QQ playlists are the primary supported path.
- `--qq-cookie` can pass a QQ Music cookie for private playlist experiments, but private playlist behavior is not deeply customized yet.
- Matching is intentionally tolerant because Chinese tracks, live versions, romanized names, and translated titles often differ between platforms.
