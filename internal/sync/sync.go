package sync

import (
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rainbowsmaug/uplink-rgl/internal/apollo"
	"github.com/rainbowsmaug/uplink-rgl/internal/epic"
	"github.com/rainbowsmaug/uplink-rgl/internal/library"
	"github.com/rainbowsmaug/uplink-rgl/internal/steam"
)

func SyncLibrary(apolloClient *apollo.Client, excluded []string) error {
	excludedIDs := make(map[string]bool, len(excluded))
	for _, id := range excluded {
		excludedIDs[id] = true
	}

	// ── Parse Steam ───────────────────────────────────────────────────────────
	fmt.Println("Finding Steam libraries...")
	steamPaths, err := steam.FindSteamLibraries()
	if err != nil {
		fmt.Println("Warning: Steam library not found:", err)
	}
	fmt.Printf("Found %d Steam library location(s)\n", len(steamPaths))

	var games []library.Game
	if len(steamPaths) > 0 {
		fmt.Println("Parsing Steam games...")
		steamGames, err := steam.ParseACFFiles(steamPaths...)
		if err != nil {
			fmt.Println("Warning: failed to parse Steam games:", err)
		}
		games = append(games, steamGames...)
	}

	// ── Parse Epic ────────────────────────────────────────────────────────────
	fmt.Println("Parsing Epic Games library...")
	epicGames, err := epic.ParseManifests(epic.DefaultManifestsDir)
	if err != nil {
		fmt.Println("Warning: Epic Games library not found:", err)
	} else {
		fmt.Printf("Found %d Epic game(s)\n", len(epicGames))
		games = append(games, epicGames...)
	}

	fmt.Println("Fetching existing Apollo apps...")
	existingApps, err := apolloClient.GetApps()
	if errors.Is(err, apollo.ErrUnauthorized) {
		fmt.Println("Session expired, re-authenticating...")
		if err2 := apolloClient.Login(); err2 != nil {
			return fmt.Errorf("re-authentication failed: %w", err2)
		}
		existingApps, err = apolloClient.GetApps()
	}
	if err != nil {
		return fmt.Errorf("failed to get Apollo apps: %w", err)
	}

	coversDir := DetectCoversDir(existingApps)
	fmt.Printf("Using covers directory: %s\n", coversDir)

	existing := make(map[string]bool)
	for _, app := range existingApps {
		existing[app.Name] = true
	}

	// Build lookup maps for backfill and uninstall detection.
	gamesByName := make(map[string]library.Game)
	steamIDs := make(map[string]bool)
	epicIDs := make(map[string]bool)
	for _, game := range games {
		gamesByName[game.Name] = game
		switch game.Source {
		case "steam":
			steamIDs[game.ID] = true
		case "epic":
			epicIDs[game.ID] = true
		}
	}

	removed := 0

	// Remove Apollo apps that are excluded or no longer installed.
	for _, app := range existingApps {
		if steamID := steam.IDFromCmd(app.Cmd); steamID != "" {
			if !excludedIDs[steamID] && steamIDs[steamID] {
				continue // still installed, not excluded
			}
			reason := "excluded"
			if !excludedIDs[steamID] {
				reason = "uninstalled"
			}
			if err := apolloClient.DeleteApp(app.UUID); err != nil {
				fmt.Printf("Failed to remove %s app %s: %v\n", reason, app.Name, err)
				continue
			}
			fmt.Printf("Removed %s game: %s\n", reason, app.Name)
			removed++
		} else if epicID := epic.IDFromCmd(app.Cmd); epicID != "" {
			if !excludedIDs[epicID] && epicIDs[epicID] {
				continue
			}
			reason := "excluded"
			if !excludedIDs[epicID] {
				reason = "uninstalled"
			}
			if err := apolloClient.DeleteApp(app.UUID); err != nil {
				fmt.Printf("Failed to remove %s app %s: %v\n", reason, app.Name, err)
				continue
			}
			fmt.Printf("Removed %s game: %s\n", reason, app.Name)
			removed++
		}
	}

	added := 0
	for _, game := range games {
		if excludedIDs[game.ID] {
			fmt.Printf("Skipping %s (excluded)\n", game.Name)
			continue
		}
		if existing[game.Name] {
			fmt.Printf("Skipping %s (already exists)\n", game.Name)
			continue
		}

		coverFile := game.Source + "_" + game.ID + ".png"
		coverPath, err := downloadCover(coverFile, game.CoverURL, coversDir)
		if err != nil {
			fmt.Printf("Warning: could not download cover for %s: %v\n", game.Name, err)
			coverPath = ""
		}

		var cmd string
		switch game.Source {
		case "steam":
			cmd = "steam://rungameid/" + game.ID
		case "epic":
			cmd = "com.epicgames.launcher://apps/" + game.ID + "?action=launch&silent=true"
		}

		app := apollo.App{
			Name:     game.Name,
			ImageURL: coverPath,
			Cmd:      cmd,
		}

		if err := apolloClient.AddApp(app); err != nil {
			fmt.Printf("Failed to add %s: %v\n", game.Name, err)
			continue
		}

		fmt.Printf("Added %s\n", game.Name)
		added++
	}

	// Backfill covers for existing apps that are missing one.
	fmt.Println("Checking for missing covers...")
	backfilled := 0
	for _, apolloApp := range existingApps {
		// Skip apps that already have a local cover file on disk.
		if apolloApp.ImageURL != "" && !strings.HasPrefix(apolloApp.ImageURL, "http") {
			if _, err := os.Stat(apolloApp.ImageURL); err == nil {
				continue
			}
		}

		game, ok := gamesByName[apolloApp.Name]
		if !ok {
			continue
		}

		coverFile := game.Source + "_" + game.ID + ".png"
		localPath, err := downloadCover(coverFile, game.CoverURL, coversDir)
		if err != nil {
			fmt.Printf("Warning: could not download cover for %s: %v\n", apolloApp.Name, err)
			continue
		}

		apolloApp.ImageURL = localPath
		if err := apolloClient.UpdateApp(apolloApp); err != nil {
			fmt.Printf("Failed to update cover for %s: %v\n", apolloApp.Name, err)
			continue
		}

		fmt.Printf("Backfilled cover for %s\n", apolloApp.Name)
		backfilled++
	}

	fmt.Printf("Sync complete. Added %d new games, backfilled %d covers, removed %d excluded.\n", added, backfilled, removed)

	// Apollo has a bug where API calls cause it to rewrite sunshine_state.json
	// with boolean values serialized as strings, which crashes it on restart.
	// Fix it automatically after every sync.
	configDir := filepath.Dir(coversDir)
	if err := fixStateFile(filepath.Join(configDir, "sunshine_state.json")); err != nil {
		fmt.Printf("Warning: could not fix Apollo state file: %v\n", err)
	}

	return nil
}

// DetectCoversDir scans existing apps for a local cover path to determine
// where Apollo stores its covers. Falls back to the default Apollo install path.
func DetectCoversDir(apps []apollo.App) string {
	for _, app := range apps {
		p := app.ImageURL
		if p == "" || strings.HasPrefix(p, "http") || !strings.ContainsAny(p, `/\`) {
			continue
		}
		p = strings.ReplaceAll(p, "/", `\`)
		if idx := strings.LastIndex(p, `\`); idx > 0 {
			return p[:idx]
		}
	}
	return `C:\Program Files\Apollo\config\covers`
}

// boolStringRe matches Apollo's broken boolean-as-string fields so fixStateFile
// can correct them in a single pass regardless of which fields are affected.
var boolStringRe = regexp.MustCompile(`"(allow_client_commands|always_use_virtual_display|enable_legacy_ordering)": "(true|false)"`)

// fixStateFile corrects Apollo's sunshine_state.json after API calls cause it
// to serialize boolean fields as strings, which crashes Apollo on restart.
func fixStateFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	fixed := boolStringRe.ReplaceAllString(string(data), `"$1": $2`)
	if fixed == string(data) {
		return nil
	}

	return os.WriteFile(path, []byte(fixed), 0644)
}

func downloadCover(coverFile, imageURL, coversDir string) (string, error) {
	if imageURL == "" {
		return "", fmt.Errorf("no cover URL")
	}
	if err := os.MkdirAll(coversDir, 0755); err != nil {
		return "", fmt.Errorf("creating covers directory: %w", err)
	}

	resp, err := http.Get(imageURL)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	img, _, err := image.Decode(resp.Body)
	if err != nil {
		return "", fmt.Errorf("decoding cover image: %w", err)
	}

	localPath := filepath.Join(coversDir, coverFile)
	tmp := localPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return "", fmt.Errorf("creating cover file: %w", err)
	}

	if err := png.Encode(f, img); err != nil {
		f.Close()
		os.Remove(tmp)
		return "", fmt.Errorf("encoding cover as png: %w", err)
	}
	f.Close()

	if err := os.Rename(tmp, localPath); err != nil {
		os.Remove(tmp)
		return "", fmt.Errorf("renaming cover file: %w", err)
	}

	return localPath, nil
}
