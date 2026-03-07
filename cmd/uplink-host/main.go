// To regenerate the Windows app icon resource (rsrc_windows_amd64.syso):
//
//	go run ./tools/mkicon
package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/getlantern/systray"
	"github.com/rainbowsmaug/uplink-rgl/internal/apollo"
	"github.com/rainbowsmaug/uplink-rgl/internal/credentials"
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

	doSync := func() {
		if err := uplinkSync.SyncLibrary(client, creds.Excluded); err != nil {
			fmt.Println("Sync failed:", err)
		}
	}

	systray.Run(
		func() { onReady(doSync) },
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

func onReady(doSync func()) {
	systray.SetIcon(icon.ICO())
	systray.SetTooltip("Uplink RGL")

	mSync := systray.AddMenuItem("Sync Now", "Sync Steam library now")
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
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
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

	if len(sources) == 0 {
		return nil, fmt.Errorf("no launcher directories could be watched")
	}
	return sources, nil
}

