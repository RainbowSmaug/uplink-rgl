package epic

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/rainbowsmaug/uplink-rgl/internal/library"
)

const DefaultManifestsDir = `C:\ProgramData\Epic\EpicGamesLauncher\Data\Manifests`

type manifest struct {
	DisplayName       string `json:"DisplayName"`
	AppName           string `json:"AppName"`
	MainGameAppName   string `json:"MainGameAppName"`
	VaultThumbnailUrl string `json:"VaultThumbnailUrl"`
	BIsIncomplete     bool   `json:"bIsIncompleteInstall"`
}

// ParseManifests scans the Epic Games manifests directory and returns installed games.
// DLC and incomplete installs are skipped.
func ParseManifests(dir string) ([]library.Game, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	var games []library.Game
	seen := make(map[string]bool)

	for _, entry := range entries {
		if !strings.EqualFold(filepath.Ext(entry.Name()), ".item") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue
		}
		var m manifest
		if err := json.Unmarshal(data, &m); err != nil {
			continue
		}
		if m.BIsIncomplete || m.DisplayName == "" || m.AppName == "" {
			continue
		}
		// Skip DLC/add-ons: MainGameAppName differs from AppName
		if m.MainGameAppName != "" && m.MainGameAppName != m.AppName {
			continue
		}
		if seen[m.AppName] {
			continue
		}
		seen[m.AppName] = true
		games = append(games, library.Game{
			ID:       m.AppName,
			Name:     m.DisplayName,
			CoverURL: m.VaultThumbnailUrl,
			Source:   "epic",
		})
	}
	return games, nil
}

// IDFromCmd extracts the Epic AppName from a com.epicgames.launcher://apps/<AppName>?... URI.
// Returns an empty string if the command is not an Epic launch URI.
func IDFromCmd(cmd string) string {
	const prefix = "com.epicgames.launcher://apps/"
	if !strings.HasPrefix(cmd, prefix) {
		return ""
	}
	rest := cmd[len(prefix):]
	if idx := strings.IndexAny(rest, "?&"); idx != -1 {
		return rest[:idx]
	}
	return rest
}
