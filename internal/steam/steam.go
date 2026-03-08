package steam

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/rainbowsmaug/uplink-rgl/internal/library"
)

// FindSteamLibraries returns all Steam library folders (steamapps directories),
// including the default install location and any additional ones configured in
// libraryfolders.vdf.
func FindSteamLibraries() ([]string, error) {
	programFiles := os.Getenv("PROGRAMFILES(X86)")
	defaultPath := filepath.Join(programFiles, "Steam", "steamapps")

	if _, err := os.Stat(defaultPath); err != nil {
		return nil, err
	}

	dirs := []string{defaultPath}

	// Parse libraryfolders.vdf for additional library paths.
	vdfPath := filepath.Join(defaultPath, "libraryfolders.vdf")
	data, err := os.ReadFile(vdfPath)
	if err == nil {
		for _, extra := range parseLibraryFolders(string(data)) {
			p := filepath.Join(extra, "steamapps")
			if _, err := os.Stat(p); err == nil {
				dirs = append(dirs, p)
			}
		}
	}

	return dirs, nil
}

// IDFromCmd extracts the Steam App ID from a steam://rungameid/<id> command string.
// Returns an empty string if the command is not a Steam launch URL.
func IDFromCmd(cmd string) string {
	const prefix = "steam://rungameid/"
	if !strings.HasPrefix(cmd, prefix) {
		return ""
	}
	return cmd[len(prefix):]
}

// parseLibraryFolders extracts additional Steam library paths from libraryfolders.vdf.
func parseLibraryFolders(data string) []string {
	var paths []string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "\"path\"") {
			continue
		}
		// Format:  "path"   "D:\\SteamLibrary"
		parts := strings.SplitN(line, "\"", -1)
		// parts: ["", "path", "\t\t", "D:\\SteamLibrary", ""]
		if len(parts) >= 4 {
			p := strings.ReplaceAll(parts[3], `\\`, `\`)
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	return paths
}

// ParseACFFiles scans one or more steamapps directories and returns all installed games.
func ParseACFFiles(steamPaths ...string) ([]library.Game, error) {
	var games []library.Game
	seen := make(map[string]bool)

	for _, steamPath := range steamPaths {
		entries, err := os.ReadDir(steamPath)
		if err != nil {
			continue
		}
		for _, entry := range entries {
			if filepath.Ext(entry.Name()) != ".acf" {
				continue
			}
			data, err := os.ReadFile(filepath.Join(steamPath, entry.Name()))
			if err != nil {
				continue
			}
			game := parseACF(string(data))
			if game != nil && !seen[game.ID] {
				seen[game.ID] = true
				games = append(games, *game)
			}
		}
	}

	return games, nil
}

func parseACF(data string) *library.Game {
    appid := extractACFValue(data, "appid")
    name := extractACFValue(data, "name")

    if appid == "" || name == "" {
        return nil
    }

    return &library.Game{
        ID:       appid,
        Name:     name,
        CoverURL: "https://steamcdn-a.akamaihd.net/steam/apps/" + appid + "/library_600x900.jpg",
        Source:   "steam",
    }
}

func extractACFValue(data, key string) string {
    needle := "\"" + key + "\""
    idx := strings.Index(data, needle)
    if idx == -1 {
        return ""
    }

    rest := data[idx+len(needle):]
    start := strings.Index(rest, "\"")
    if start == -1 {
        return ""
    }

    rest = rest[start+1:]
    end := strings.Index(rest, "\"")
    if end == -1 {
        return ""
    }

    return rest[:end]
}
