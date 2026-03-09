# uplink-rgl

A self-hosted remote gaming link — automatically keeps your game library in sync across your devices so you can pick up and play from anywhere without manual setup.

The host agent runs on your gaming PC and syncs your installed games to [Apollo](https://github.com/ClassicOldSong/Apollo) (a [Sunshine](https://github.com/LizardByte/Sunshine) fork), making them available to stream via [Moonlight](https://moonlight-stream.org/). The client is a Wayland overlay launcher that presents your synced library and launches games in one keypress.

## Status

| Component | Status |
|-----------|--------|
| Host agent (Steam + Epic → Apollo) | ✅ Working |
| Wayland overlay client (Quickshell) | ✅ Working |
| Auto-start on Windows boot | ✅ Working |
| GOG / other launcher support | 🚧 Planned |
| Exclusion list UI | 🚧 Planned |

## Features

**Host (Windows)**
- Syncs Steam and Epic Games libraries to Apollo automatically
- Detects installs and uninstalls in real time via file watchers
- Downloads and converts cover art automatically
- Serves cover art over HTTP for the client overlay
- Exclusion list to hide specific games from your stream library
- Auto-start on login via tray menu toggle
- Logs to `%LOCALAPPDATA%\uplink-rgl\uplink-host.log`

**Client (Linux/Wayland)**
- Full-screen game launcher overlay toggled with a keybind
- Parallelogram card carousel with cover art
- Launcher badges (Steam/Epic) to differentiate duplicate titles
- Two-phase loading: game list appears instantly, cover art loads in background
- Search bar (`f` to activate), keyboard navigation (`h`/`l` or arrow keys)
- First-launch setup form for Apollo credentials — no config file editing needed

## Prerequisites

**Gaming PC (Windows)**
- [Apollo](https://github.com/ClassicOldSong/Apollo) installed and running
- Steam and/or Epic Games Launcher installed

**Client (Linux)**
- [Quickshell](https://quickshell.outfoxxed.me) — Wayland overlay runtime
- [Moonlight](https://moonlight-stream.org) — game streaming client

## Setup

### Gaming PC

1. Install and configure [Apollo](https://github.com/ClassicOldSong/Apollo)
2. Run `uplink-host.exe` — on first launch it will prompt for your Apollo credentials
3. Your library will sync and remain in sync automatically
4. Optional: click **Enable Auto-start** in the tray menu to start on login

### Client

```bash
# Build
go build ./cmd/uplink-client/

# Install (checks for quickshell and moonlight, copies overlay files)
./uplink-client install

# Wire a keybind in your compositor to toggle the overlay, e.g. Niri:
# Mod+S { spawn "sh" "-c" "~/.local/bin/uplink-toggle"; }
```

On first launch, the overlay will show a setup form to enter your gaming PC's IP and Apollo credentials.

## Adding a new launcher

1. Add `internal/<launcher>/` — parse manifests, return `[]library.Game`, add `IDFromCmd`
2. Wire into `internal/sync/sync.go` — merge games, add launch URI format, add cover filename prefix
3. Add a `watcher.Source` in `buildWatchSources()` in `cmd/uplink-host/main.go`
4. Detect the launch URI in `appsToGames()` in `cmd/uplink-client/main.go` and set `Launcher` field
5. Add a `launcher_<name>.png` icon to `cmd/uplink-client/quickshell/`

## Roadmap

- Exclusion list management UI
- Epic Games cover art (VaultThumbnailUrl is often empty — needs SteamGridDB/IGDB fallback)
- GOG, Xbox Game Pass, and other launcher support
