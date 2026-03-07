# uplink-rgl

A self-hosted remote gaming link — automatically keeps your game library in sync across your devices so you can pick up and play from anywhere without manual setup.

The host agent runs on your gaming PC and syncs your installed games to [Apollo](https://github.com/ClassicOldSong/Apollo) (a [Sunshine](https://github.com/LizardByte/Sunshine) fork), making them available to stream via [Moonlight](https://moonlight-stream.org/). New installs and uninstalls are detected automatically. A client application is planned to complement the Moonlight experience on the other end.

## Status

Early development. Host-side sync is functional.

| Component | Status |
|-----------|--------|
| Host agent (Steam → Apollo) | ✅ Working |
| Client application | 🚧 Planned |

## Features

- Detects and syncs newly installed and uninstalled Steam games in real time
- Downloads and converts cover art automatically
- Supports multiple Steam library locations
- Exclusion list to hide specific games from your stream library
- Runs silently in the system tray

## Setup

1. Install and configure [Apollo](https://github.com/ClassicOldSong/Apollo) on your gaming PC
2. Run `uplink-host.exe` — on first launch it will prompt for your Apollo credentials
3. Your Steam library will sync and remain in sync automatically

## Roadmap

- Exclusion list management UI
- Auto-start on boot
- Additional launcher support (Epic Games, GOG, etc.)
- Client application
