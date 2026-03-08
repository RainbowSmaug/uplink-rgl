package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"image/jpeg"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rainbowsmaug/uplink-rgl/internal/apollo"
	"github.com/rainbowsmaug/uplink-rgl/internal/credentials"
	"github.com/rainbowsmaug/uplink-rgl/internal/epic"
	"github.com/rainbowsmaug/uplink-rgl/internal/steam"
)

//go:embed quickshell
var quickshellFS embed.FS

// GameData is the JSON record emitted by the "games" subcommand.
type GameData struct {
	UUID      string `json:"uuid"`
	Name      string `json:"name"`
	SteamID   string `json:"steamID"`
	Launcher  string `json:"launcher,omitempty"`  // "steam", "epic", etc. — empty for non-game entries
	ImageName string `json:"imageName,omitempty"` // basename of Apollo's image-path
	CoverPath string `json:"coverPath,omitempty"` // absolute path to locally cached cover PNG
}

var steamIDRe = regexp.MustCompile(`steam_(\d+)\.png`)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: uplink-client <games|launch|configure> [args...]")
		os.Exit(1)
	}

	switch os.Args[1] {
	case "games":
		cmdGames()
	case "games-covers":
		cmdGameCovers()
	case "launch":
		if len(os.Args) < 3 {
			fmt.Fprintln(os.Stderr, "usage: uplink-client launch <game name>")
			os.Exit(1)
		}
		cmdLaunch(strings.Join(os.Args[2:], " "))
	case "configure":
		if len(os.Args) < 5 {
			fmt.Fprintln(os.Stderr, "usage: uplink-client configure <host> <username> <password>")
			os.Exit(1)
		}
		cmdConfigure(os.Args[2], os.Args[3], os.Args[4])
	case "install":
		cmdInstall()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", os.Args[1])
		os.Exit(1)
	}
}

// apolloClient creates an Apollo client, reusing a cached session cookie if
// available. On 401 it re-authenticates and saves the new cookie.
func apolloClient(creds *credentials.Credentials, sessionPath string) (*apollo.Client, []apollo.App, error) {
	baseURL := "https://" + creds.HostAddress + ":47990"
	client := apollo.NewClient(baseURL, creds.Username, creds.Password)

	// Try with cached session first; re-auth only on 401.
	client.LoadCookies(sessionPath) // ignore error (first run or missing file)
	apps, err := client.GetApps()
	if err != nil {
		if !errors.Is(err, apollo.ErrUnauthorized) {
			return nil, nil, fmt.Errorf("fetching games: %w", err)
		}
		if err2 := client.Login(); err2 != nil {
			return nil, nil, fmt.Errorf("Apollo login failed: %w", err2)
		}
		client.SaveCookies(sessionPath) // best-effort
		apps, err = client.GetApps()
		if err != nil {
			return nil, nil, fmt.Errorf("fetching games: %w", err)
		}
	}
	return client, apps, nil
}

func cmdGames() {
	creds, err := credentials.Load()
	if err != nil || creds.HostAddress == "" {
		json.NewEncoder(os.Stdout).Encode(map[string]string{"error": "not_configured"})
		return
	}

	cacheDir, _ := ensureCoverCacheDir()
	sessionPath := cacheDir + "/session.json"

	_, apps, err := apolloClient(creds, sessionPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	games := appsToGames(apps)
	json.NewEncoder(os.Stdout).Encode(games)
}

// cmdGameCovers downloads/checks covers for each game and outputs only
// uuid+coverPath pairs. QML runs this after the fast games list is shown.
func cmdGameCovers() {
	creds, err := credentials.Load()
	if err != nil || creds.HostAddress == "" {
		json.NewEncoder(os.Stdout).Encode([]struct{}{})
		return
	}

	cacheDir, err := ensureCoverCacheDir()
	if err != nil {
		os.Exit(1)
	}
	sessionPath := cacheDir + "/session.json"

	_, apps, err := apolloClient(creds, sessionPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	games := appsToGames(apps)
	coversBaseURL := "http://" + creds.HostAddress + ":47991/"

	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)

	for i := range games {
		if games[i].ImageName == "" && games[i].SteamID == "" {
			continue
		}
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var p string
			// Prefer the image already set in Apollo — served directly by
			// uplink-host's covers file server on port 47991.
			if games[i].ImageName != "" {
				p = cachedApolloImage(games[i].ImageName, coversBaseURL, cacheDir)
			}
			// Fall back to Steam CDN for any game with a known Steam App ID.
			if p == "" && games[i].SteamID != "" {
				p = cachedCover(games[i].SteamID, cacheDir)
			}
			if p != "" {
				mu.Lock()
				games[i].CoverPath = p
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	json.NewEncoder(os.Stdout).Encode(games)
}

func appsToGames(apps []apollo.App) []GameData {
	games := make([]GameData, 0, len(apps))
	for _, app := range apps {
		g := GameData{UUID: app.UUID, Name: app.Name}
		g.ImageName = imageBasename(app.ImageURL)
		if m := steamIDRe.FindStringSubmatch(app.ImageURL); m != nil {
			g.SteamID = m[1]
			g.Launcher = "steam"
		} else if id := steam.IDFromCmd(app.Cmd); id != "" {
			g.SteamID = id
			g.Launcher = "steam"
		} else if epic.IDFromCmd(app.Cmd) != "" {
			g.Launcher = "epic"
		}
		games = append(games, g)
	}
	sort.Slice(games, func(i, j int) bool {
		return games[i].Name < games[j].Name
	})
	return games
}

// imageBasename extracts the filename from a Windows or Unix path.
func imageBasename(p string) string {
	if p == "" {
		return ""
	}
	return path.Base(strings.ReplaceAll(p, `\`, "/"))
}

// ensureCoverCacheDir returns (and creates if needed) ~/.cache/uplink-rgl/covers.
func ensureCoverCacheDir() (string, error) {
	base, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "uplink-rgl", "covers")
	return dir, os.MkdirAll(dir, 0755)
}

// cachedApolloImage fetches a cover by filename from uplink-host's covers
// file server (port 47991). Files are already PNG — no transcoding needed.
func cachedApolloImage(imageName, baseURL, cacheDir string) string {
	localPath := filepath.Join(cacheDir, imageName)

	if info, err := os.Stat(localPath); err == nil {
		if time.Since(info.ModTime()) < 24*time.Hour {
			return localPath
		}
	}

	resp, err := http.Get(baseURL + imageName)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		if _, err := os.Stat(localPath); err == nil {
			return localPath // stale cache beats nothing
		}
		return ""
	}
	defer resp.Body.Close()

	tmp := localPath + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return ""
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return ""
	}
	f.Close()
	if err := os.Rename(tmp, localPath); err != nil {
		os.Remove(tmp)
		return ""
	}
	return localPath
}

// steamCoverURLs returns CDN URLs to try in order for a given Steam App ID,
// ending with a Steam Store API lookup to get the current hashed image URL for
// games whose assets have been moved off the legacy CDN paths.
func steamCoverURLs(steamID string) []string {
	base := "https://steamcdn-a.akamaihd.net/steam/apps/" + steamID + "/"
	urls := []string{
		base + "library_600x900.jpg",
		base + "library_600x900_2x.jpg",
		base + "header.jpg",
	}
	// Newer games (or games whose assets were reorganised) only have
	// content-addressed URLs under shared.akamai.steamstatic.com. We can
	// discover the current URL via the store API.
	if u := steamStoreHeaderURL(steamID); u != "" {
		urls = append(urls, u)
	}
	return urls
}

// steamStoreHeaderURL fetches the header_image URL from the Steam Store API.
// Returns "" on any error so callers can treat it as a missing URL.
func steamStoreHeaderURL(steamID string) string {
	url := "https://store.steampowered.com/api/appdetails?appids=" + steamID + "&filters=basic,header_image"
	resp, err := http.Get(url)
	if err != nil || resp.StatusCode != http.StatusOK {
		if resp != nil {
			resp.Body.Close()
		}
		return ""
	}
	defer resp.Body.Close()

	var result map[string]struct {
		Success bool `json:"success"`
		Data    struct {
			HeaderImage string `json:"header_image"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}
	if entry, ok := result[steamID]; ok && entry.Success {
		return entry.Data.HeaderImage
	}
	return ""
}

// cachedCover returns the local path to a game's cover PNG, downloading/refreshing
// from Steam CDN if the cached file is missing or older than 24 hours.
func cachedCover(steamID, cacheDir string) string {
	localPath := filepath.Join(cacheDir, "steam_"+steamID+".png")

	if info, err := os.Stat(localPath); err == nil {
		if time.Since(info.ModTime()) < 24*time.Hour {
			return localPath
		}
	}

	for _, url := range steamCoverURLs(steamID) {
		resp, err := http.Get(url)
		if err != nil || resp.StatusCode != http.StatusOK {
			if resp != nil {
				resp.Body.Close()
			}
			continue
		}

		img, err := jpeg.Decode(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}

		// Write to a temp file then rename so we never leave a partial PNG.
		tmp := localPath + ".tmp"
		f, err := os.Create(tmp)
		if err != nil {
			return ""
		}
		if err := png.Encode(f, img); err != nil {
			f.Close()
			os.Remove(tmp)
			return ""
		}
		f.Close()
		if err := os.Rename(tmp, localPath); err != nil {
			os.Remove(tmp)
			return ""
		}
		return localPath
	}

	// Return stale cache if all downloads fail rather than nothing
	if _, err := os.Stat(localPath); err == nil {
		return localPath
	}
	return ""
}

func cmdLaunch(gameName string) {
	creds, err := credentials.Load()
	if err != nil || creds.HostAddress == "" {
		fmt.Fprintln(os.Stderr, "not configured")
		os.Exit(1)
	}

	cmd := exec.Command("moonlight", "stream", creds.HostAddress, gameName)
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "launching moonlight: %v\n", err)
		os.Exit(1)
	}
}

func cmdConfigure(host, username, password string) {
	h := strings.TrimPrefix(host, "https://")
	h = strings.TrimPrefix(h, "http://")
	h = strings.TrimRight(h, "/")
	if idx := strings.LastIndex(h, ":"); idx != -1 {
		h = h[:idx]
	}

	apolloURL := "https://" + h + ":47990"
	client := apollo.NewClient(apolloURL, username, password)
	if err := client.Login(); err != nil {
		fmt.Fprintf(os.Stderr, "could not connect to Apollo at %s: %v\n", apolloURL, err)
		os.Exit(1)
	}

	creds := &credentials.Credentials{
		HostAddress: h,
		Username:    username,
		Password:    password,
	}
	if err := credentials.Save(creds); err != nil {
		fmt.Fprintf(os.Stderr, "saving credentials: %v\n", err)
		os.Exit(1)
	}

	fmt.Println("ok")
}

func cmdInstall() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot find home directory:", err)
		os.Exit(1)
	}

	// ── 0. Pre-flight dependency check ───────────────────────────────────────
	fmt.Println("checking dependencies...")
	ok := true
	for _, dep := range []struct{ bin, hint string }{
		{"quickshell", "https://quickshell.outfoxxed.me"},
		{"moonlight", "https://moonlight-stream.org or your package manager"},
	} {
		if _, err := exec.LookPath(dep.bin); err != nil {
			fmt.Printf("  ✗ %s not found — install from %s\n", dep.bin, dep.hint)
			ok = false
		} else {
			fmt.Printf("  ✓ %s\n", dep.bin)
		}
	}
	if !ok {
		fmt.Println("\nInstall missing dependencies and re-run to continue.")
		os.Exit(1)
	}
	fmt.Println()

	// ── 1. Install Quickshell overlay files ──────────────────────────────────
	qsDir := filepath.Join(home, ".config", "quickshell", "uplink")
	if err := os.MkdirAll(qsDir, 0755); err != nil {
		fmt.Fprintln(os.Stderr, "creating quickshell dir:", err)
		os.Exit(1)
	}

	entries, err := fs.ReadDir(quickshellFS, "quickshell")
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading embedded quickshell files:", err)
		os.Exit(1)
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		data, err := quickshellFS.ReadFile("quickshell/" + e.Name())
		if err != nil {
			fmt.Fprintln(os.Stderr, "reading embedded file:", err)
			os.Exit(1)
		}
		dest := filepath.Join(qsDir, e.Name())
		if err := os.WriteFile(dest, data, 0644); err != nil {
			fmt.Fprintln(os.Stderr, "writing", dest, ":", err)
			os.Exit(1)
		}
		fmt.Println("installed", dest)
	}

	// ── 2. Install binary to ~/.local/bin ────────────────────────────────────
	binDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(binDir, 0755); err != nil {
		fmt.Fprintln(os.Stderr, "creating bin dir:", err)
		os.Exit(1)
	}
	selfPath, err := os.Executable()
	if err != nil {
		fmt.Fprintln(os.Stderr, "cannot determine executable path:", err)
		os.Exit(1)
	}
	destBin := filepath.Join(binDir, "uplink-client")
	if err := copyFile(selfPath, destBin, 0755); err != nil {
		fmt.Fprintln(os.Stderr, "installing binary:", err)
		os.Exit(1)
	}
	fmt.Println("installed", destBin)

	fmt.Println()
	fmt.Println("Done. To launch the overlay, run:")
	fmt.Println("  quickshell -c ~/.config/quickshell/uplink")
	fmt.Println()
	fmt.Println("Wire that command to a keybind in your compositor however you like.")
}

func copyFile(src, dst string, mode fs.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}
