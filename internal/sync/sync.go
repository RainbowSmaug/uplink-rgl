package sync

import (
	"fmt"
	"image/jpeg"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/rainbowsmaug/uplink-rgl/internal/apollo"
	"github.com/rainbowsmaug/uplink-rgl/internal/library"
	"github.com/rainbowsmaug/uplink-rgl/internal/steam"
)

func SyncLibrary(apolloClient *apollo.Client, excluded []string) error {
	excludedIDs := make(map[string]bool, len(excluded))
	for _, id := range excluded {
		excludedIDs[id] = true
	}
	fmt.Println("Finding Steam libraries...")
	steamPaths, err := steam.FindSteamLibraries()
	if err != nil {
		return fmt.Errorf("failed to find Steam library: %w", err)
	}
	fmt.Printf("Found %d Steam library location(s)\n", len(steamPaths))

	fmt.Println("Parsing Steam games...")
	games, err := steam.ParseACFFiles(steamPaths...)
	if err != nil {
		return fmt.Errorf("failed to parse Steam games: %w", err)
	}

	fmt.Println("Fetching existing Apollo apps...")
	existingApps, err := apolloClient.GetApps()
	if err != nil {
		return fmt.Errorf("failed to get Apollo apps: %w", err)
	}

	coversDir := DetectCoversDir(existingApps)
	fmt.Printf("Using covers directory: %s\n", coversDir)

	existing := make(map[string]bool)
	for _, app := range existingApps {
		existing[app.Name] = true
	}

	// Build lookup maps for the cover backfill pass and uninstall detection.
	steamByName := make(map[string]library.Game)
	steamIDs := make(map[string]bool)
	for _, game := range games {
		steamByName[game.Name] = game
		steamIDs[game.ID] = true
	}

	removed := 0

	// Remove Apollo apps that are excluded or no longer installed.
	for _, app := range existingApps {
		id := steam.IDFromCmd(app.Cmd)
		if id == "" || (!excludedIDs[id] && steamIDs[id]) {
			continue
		}
		reason := "excluded"
		if !excludedIDs[id] {
			reason = "uninstalled"
		}
		if err := apolloClient.DeleteApp(app.UUID); err != nil {
			fmt.Printf("Failed to remove %s app %s: %v\n", reason, app.Name, err)
			continue
		}
		fmt.Printf("Removed %s game: %s\n", reason, app.Name)
		removed++
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

		coverPath, err := downloadCover(game.ID, game.CoverURL, coversDir)
		if err != nil {
			fmt.Printf("Warning: could not download cover for %s: %v\n", game.Name, err)
			coverPath = ""
		}

		app := apollo.App{
			Name:     game.Name,
			ImageURL: coverPath,
			Cmd:      "steam://rungameid/" + game.ID,
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
		// Skip apps that already have a local cover file that exists on disk.
		if apolloApp.ImageURL != "" && !strings.HasPrefix(apolloApp.ImageURL, "http") {
			if _, err := os.Stat(apolloApp.ImageURL); err == nil {
				continue
			}
		}

		steamGame, ok := steamByName[apolloApp.Name]
		if !ok {
			continue
		}

		localPath, err := downloadCover(steamGame.ID, steamGame.CoverURL, coversDir)
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

func downloadCover(appID, imageURL, coversDir string) (string, error) {
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

	img, err := jpeg.Decode(resp.Body)
	if err != nil {
		return "", fmt.Errorf("decoding cover image: %w", err)
	}

	localPath := filepath.Join(coversDir, "steam_"+appID+".png")
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
