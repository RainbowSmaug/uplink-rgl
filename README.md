# uplink-rgl

Automatically syncs your installed Steam library to [Apollo](https://github.com/ClassicOldSong/Apollo) (a Sunshine fork) so your games appear in [Moonlight](https://moonlight-stream.org/) without manual entry.

Runs as a Windows system tray application on your gaming PC. Detects new installs and uninstalls in real time.

## Features

- **Auto-sync** — on startup and whenever Steam installs or removes a game
- **Cover art** — downloads box art from Steam and converts it to the PNG format Apollo requires
- **Exclusion list** — blacklist specific games by Steam App ID so they never appear in Moonlight
- **Multiple Steam libraries** — supports games spread across multiple drives
- **System tray** — no console window; right-click for manual sync or quit
- **Crash prevention** — automatically fixes an Apollo bug that serialises boolean config values as strings, causing it to crash on restart

## Requirements

- [Apollo](https://github.com/ClassicOldSong/Apollo) installed and running on your gaming PC
- Steam installed at the default `Program Files (x86)\Steam` location (additional library folders are picked up automatically)
- Go 1.21+ and a Linux/WSL build environment with SSH access to the gaming PC (for building; the binary itself is a standalone Windows exe)

## Building

```bash
GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o uplink-host.exe ./cmd/uplink-host/
```

Copy `uplink-host.exe` to your gaming PC and run it. On first launch a browser window opens to collect your Apollo credentials (username and password). These are saved to `%APPDATA%\uplink-rgl\config.json`.

## Configuration

Config file location: `%APPDATA%\uplink-rgl\config.json`

```json
{
  "username": "your-apollo-username",
  "password": "your-apollo-password",
  "excluded": ["228980"]
}
```

`excluded` is a list of Steam App IDs to never add to Apollo (and to remove if already present). The Steam App ID for a game can be found in its Steam store URL or via [SteamDB](https://www.steamdb.info/). `228980` is Steamworks Common Redistributables.

## Project layout

```
cmd/
  uplink-host/        Windows tray agent
  uplink-client/      (not yet implemented)

internal/
  apollo/             Apollo REST API client
  credentials/        Config file management
  icon/               Programmatic app icon
  library/            Shared Game type
  steam/              Steam library discovery and ACF parsing
  sync/               Full sync orchestration
  watcher/            File system watcher (fsnotify, debounced)

tools/
  mkicon/             Regenerates the embedded Windows exe icon
```

## Regenerating the app icon

The icon is generated programmatically in `internal/icon/icon.go` and embedded into the exe via `cmd/uplink-host/rsrc_windows_amd64.syso`. After changing the icon source:

```bash
go run ./tools/mkicon
```

## Roadmap

- Exclusion list management UI in the tray menu
- Auto-start on Windows boot via Task Scheduler
- Support for additional launchers (Epic Games, GOG)
- Client application (`cmd/uplink-client/`)
