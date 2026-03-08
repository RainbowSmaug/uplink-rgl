// To regenerate the Windows app icon resource (rsrc_windows_amd64.syso):
//
//	go run ./tools/mkicon
package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/getlantern/systray"
	"github.com/rainbowsmaug/uplink-rgl/internal/apollo"
	"github.com/rainbowsmaug/uplink-rgl/internal/credentials"
	"github.com/rainbowsmaug/uplink-rgl/internal/epic"
	"github.com/rainbowsmaug/uplink-rgl/internal/icon"
	"github.com/rainbowsmaug/uplink-rgl/internal/steam"
	uplinkSync "github.com/rainbowsmaug/uplink-rgl/internal/sync"
	"github.com/rainbowsmaug/uplink-rgl/internal/watcher"
)

func main() {
	client, creds, err := setup()
	if err != nil {
		fmt.Println(err)
		os.Exit(1)
	}

	go startCoversServer(client)

	doSync := func() {
		if err := uplinkSync.SyncLibrary(client, creds.Excluded); err != nil {
			fmt.Println("Sync failed:", err)
		}
	}

	manageExclusions := func() {
		apps, err := client.GetApps()
		if err != nil {
			fmt.Println("Could not fetch apps for exclusion management:", err)
			return
		}
		var games []credentials.ExclusionGame
		for _, app := range apps {
			id := steam.IDFromCmd(app.Cmd)
			if id == "" {
				continue
			}
			games = append(games, credentials.ExclusionGame{ID: id, Name: app.Name})
		}
		newExcluded, err := credentials.PromptExclusions(games, creds.Excluded)
		if err != nil {
			fmt.Println("Exclusion management failed:", err)
			return
		}
		creds.Excluded = newExcluded
		if err := credentials.Save(creds); err != nil {
			fmt.Println("Warning: could not save exclusions:", err)
		}
		go doSync()
	}

	systray.Run(
		func() { onReady(doSync, manageExclusions) },
		func() {},
	)
}

func setup() (*apollo.Client, *credentials.Credentials, error) {
	creds, err := credentials.Load()
	if err != nil {
		if !errors.Is(err, credentials.ErrNotFound) {
			return nil, nil, fmt.Errorf("failed to read credentials: %w", err)
		}
		fmt.Println("No stored credentials found. Launching setup...")
		creds, err = credentials.Prompt()
		if err != nil {
			return nil, nil, fmt.Errorf("credential setup failed: %w", err)
		}
		if err := credentials.Save(creds); err != nil {
			fmt.Println("Warning: could not save credentials:", err)
		}
	}

	client := apollo.NewClient(credentials.ApolloBaseURL, creds.Username, creds.Password)
	if err := client.Login(); err != nil {
		return nil, nil, fmt.Errorf("login failed: %w", err)
	}
	fmt.Println("Logged in successfully.")
	return client, creds, nil
}

func onReady(doSync func(), manageExclusions func()) {
	systray.SetIcon(icon.ICO())
	systray.SetTooltip("Uplink RGL")

	mSync := systray.AddMenuItem("Sync Now", "Sync Steam library now")
	mExclusions := systray.AddMenuItem("Manage Exclusions", "Choose which games to hide from Moonlight")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "Stop Uplink RGL")

	// Background: initial sync then watch for library changes.
	go func() {
		fmt.Println("Running initial sync...")
		doSync()

		sources, err := buildWatchSources()
		if err != nil {
			fmt.Println("Warning: could not set up file watchers:", err)
			return
		}
		fmt.Println("Watching for library changes...")
		if err := watcher.Run(sources, 10*time.Second, doSync); err != nil {
			fmt.Println("Watcher stopped:", err)
		}
	}()

	// Handle tray menu events.
	go func() {
		for {
			select {
			case <-mSync.ClickedCh:
				go doSync()
			case <-mExclusions.ClickedCh:
				go manageExclusions()
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
}

// startCoversServer serves the Apollo covers directory over plain HTTP on port
// 47991, automatically adding a Windows Firewall inbound rule on first run.
func startCoversServer(client *apollo.Client) {
	ensureCoversFirewallRule()

	coversDir := `C:\Program Files\Apollo\config\covers`
	if apps, err := client.GetApps(); err == nil {
		coversDir = uplinkSync.DetectCoversDir(apps)
	}
	fmt.Println("Serving covers on :47991 from", coversDir)
	if err := http.ListenAndServe(":47991", http.FileServer(http.Dir(coversDir))); err != nil {
		fmt.Println("covers server error:", err)
	}
}

// ensureCoversFirewallRule adds a Windows Firewall inbound rule for port 47991
// if one doesn't already exist. Elevation is handled via a UAC prompt (once).
func ensureCoversFirewallRule() {
	const ruleName = "Uplink RGL Covers"

	// Read-only check — no elevation needed.
	out, _ := exec.Command("netsh", "advfirewall", "firewall", "show", "rule",
		"name="+ruleName).Output()
	if strings.Contains(string(out), ruleName) {
		return // already present
	}

	// Rule missing — add it via an elevated netsh call. PowerShell's
	// Start-Process -Verb RunAs triggers a one-time UAC prompt.
	fmt.Println("Adding firewall rule for covers server (one-time UAC prompt)...")
	err := exec.Command("powershell", "-Command",
		`Start-Process -FilePath netsh `+
			`-ArgumentList 'advfirewall firewall add rule name="`+ruleName+`" protocol=TCP dir=in localport=47991 action=allow' `+
			`-Verb RunAs -Wait`,
	).Run()
	if err != nil {
		fmt.Println("Warning: could not add firewall rule:", err)
	}
}

// buildWatchSources returns a watcher.Source for each supported launcher.
// Add a new Source here to support additional game launchers.
func buildWatchSources() ([]watcher.Source, error) {
	var sources []watcher.Source

	// Steam — watch all steamapps directories for ACF manifest changes.
	if dirs, err := steam.FindSteamLibraries(); err != nil {
		fmt.Println("Warning: Steam library not found, Steam watching disabled:", err)
	} else {
		sources = append(sources, watcher.Source{
			Name: "Steam",
			Dirs: dirs,
			Filter: func(name string) bool {
				return strings.EqualFold(filepath.Ext(name), ".acf")
			},
		})
	}

	// Epic Games — watch manifests directory for .item file changes.
	if _, err := os.Stat(epic.DefaultManifestsDir); err == nil {
		sources = append(sources, watcher.Source{
			Name: "Epic",
			Dirs: []string{epic.DefaultManifestsDir},
			Filter: func(name string) bool {
				return strings.EqualFold(filepath.Ext(name), ".item")
			},
		})
	} else {
		fmt.Println("Warning: Epic Games manifests not found, Epic watching disabled")
	}

	if len(sources) == 0 {
		return nil, fmt.Errorf("no launcher directories could be watched")
	}
	return sources, nil
}

