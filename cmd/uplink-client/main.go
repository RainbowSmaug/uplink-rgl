package main

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

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
