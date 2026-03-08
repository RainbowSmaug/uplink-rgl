package credentials

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const ApolloBaseURL = "https://localhost:47990"

// ErrNotFound is returned by Load when no config file exists yet.
var ErrNotFound = errors.New("no stored credentials found")

type Credentials struct {
	Username    string   `json:"username"`
	Password    string   `json:"password"`
	Excluded    []string `json:"excluded,omitempty"`    // Steam App IDs to exclude from sync
	HostAddress string   `json:"host_address,omitempty"` // Gaming PC IP/hostname for client (ignored by host)
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("could not determine config directory: %w", err)
	}
	return filepath.Join(dir, "uplink-rgl", "config.json"), nil
}

// Load reads credentials from %APPDATA%\uplink-rgl\config.json.
// Returns ErrNotFound if no config file exists yet.
func Load() (*Credentials, error) {
	path, err := configPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var creds Credentials
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("corrupt config file: %w", err)
	}

	return &creds, nil
}

// Save writes credentials to %APPDATA%\uplink-rgl\config.json with
// user-only (0600) permissions.
func Save(creds *Credentials) error {
	path, err := configPath()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return fmt.Errorf("creating config directory: %w", err)
	}

	data, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return err
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("writing config file: %w", err)
	}

	return nil
}

// Prompt starts a localhost web server, opens the browser to a login form,
// waits for the user to submit credentials, then shuts the server down.
func Prompt() (*Credentials, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not start credential prompt: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	credsCh := make(chan *Credentials, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, loginHTML)
	})

	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		username := r.FormValue("username")
		password := r.FormValue("password")

		if username == "" || password == "" {
			http.Error(w, "username and password are required", http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, successHTML)

		credsCh <- &Credentials{Username: username, Password: password}
	})

	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	fmt.Printf("Opening browser for Apollo credentials: %s\n", addr)
	openBrowser(addr)

	select {
	case creds := <-credsCh:
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		return creds, nil
	case err := <-errCh:
		return nil, fmt.Errorf("credential prompt server error: %w", err)
	case <-time.After(5 * time.Minute):
		srv.Close()
		return nil, fmt.Errorf("timed out waiting for credentials (5 min)")
	}
}

// ExclusionGame is a minimal game descriptor used by the exclusion UI.
type ExclusionGame struct {
	ID   string // Steam App ID
	Name string
}

// PromptExclusions opens a browser UI listing all known games as checkboxes.
// Checked = excluded. Blocks until the user submits, then returns the new list.
func PromptExclusions(games []ExclusionGame, excluded []string) ([]string, error) {
	excludedSet := make(map[string]bool, len(excluded))
	for _, id := range excluded {
		excludedSet[id] = true
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("could not start exclusion prompt: %w", err)
	}

	port := listener.Addr().(*net.TCPAddr).Port
	resultCh := make(chan []string, 1)
	errCh := make(chan error, 1)

	mux := http.NewServeMux()
	srv := &http.Server{Handler: mux}

	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, buildExclusionsHTML(games, excludedSet))
	})

	mux.HandleFunc("/save", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		r.ParseForm()
		newExcluded := r.Form["excluded"]
		if newExcluded == nil {
			newExcluded = []string{}
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, successHTML)
		resultCh <- newExcluded
	})

	go func() {
		if err := srv.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	addr := fmt.Sprintf("http://127.0.0.1:%d", port)
	fmt.Printf("Opening browser for exclusion management: %s\n", addr)
	openBrowser(addr)

	select {
	case result := <-resultCh:
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		return result, nil
	case err := <-errCh:
		return nil, fmt.Errorf("exclusion prompt server error: %w", err)
	case <-time.After(10 * time.Minute):
		srv.Close()
		return nil, fmt.Errorf("timed out waiting for exclusion submission")
	}
}

func openBrowser(addr string) {
	exec.Command("cmd", "/c", "start", addr).Start()
}

func buildExclusionsHTML(games []ExclusionGame, excludedSet map[string]bool) string {
	var items strings.Builder
	for _, g := range games {
		if g.ID == "" {
			continue
		}
		checked := ""
		if excludedSet[g.ID] {
			checked = " checked"
		}
		fmt.Fprintf(&items,
			`<label class="game"><input type="checkbox" name="excluded" value="%s"%s><span>%s</span></label>`,
			html.EscapeString(g.ID), checked, html.EscapeString(g.Name),
		)
	}
	return fmt.Sprintf(exclusionsHTMLTemplate, items.String())
}

const exclusionsHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Uplink RGL - Exclusions</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #0f0f0f; color: #e0e0e0;
      display: flex; align-items: flex-start; justify-content: center;
      min-height: 100vh; padding: 2rem 1rem;
    }
    .card {
      background: #1a1a1a; border: 1px solid #2e2e2e;
      border-radius: 10px; padding: 2rem; width: 100%%; max-width: 480px;
    }
    h1 { font-size: 1.25rem; margin-bottom: 0.4rem; }
    .subtitle { font-size: 0.85rem; color: #888; margin-bottom: 1.5rem; }
    .game-list {
      max-height: 420px; overflow-y: auto;
      border: 1px solid #2e2e2e; border-radius: 6px; margin-bottom: 1.5rem;
    }
    .game {
      display: flex; align-items: center; gap: 0.75rem;
      padding: 0.6rem 0.85rem; cursor: pointer;
      border-bottom: 1px solid #242424;
    }
    .game:last-child { border-bottom: none; }
    .game:hover { background: #222; }
    .game input[type="checkbox"] { width: 16px; height: 16px; accent-color: #2563eb; cursor: pointer; flex-shrink: 0; }
    .game span { font-size: 0.9rem; }
    button {
      width: 100%%; padding: 0.65rem; background: #2563eb;
      color: #fff; border: none; border-radius: 6px; font-size: 0.95rem; cursor: pointer;
    }
    button:hover { background: #1d4ed8; }
  </style>
</head>
<body>
  <div class="card">
    <h1>Excluded Games</h1>
    <p class="subtitle">Checked games will be removed from Moonlight and excluded from future syncs.</p>
    <form method="POST" action="/save">
      <div class="game-list">%s</div>
      <button type="submit">Save</button>
    </form>
  </div>
</body>
</html>`

const loginHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Uplink RGL - Apollo Login</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #0f0f0f;
      color: #e0e0e0;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
    }
    .card {
      background: #1a1a1a;
      border: 1px solid #2e2e2e;
      border-radius: 10px;
      padding: 2.5rem 2rem;
      width: 100%;
      max-width: 380px;
    }
    h1 { font-size: 1.25rem; margin-bottom: 0.5rem; }
    p  { font-size: 0.85rem; color: #888; margin-bottom: 2rem; }
    label { display: block; font-size: 0.8rem; color: #aaa; margin-bottom: 0.35rem; }
    input {
      width: 100%;
      padding: 0.6rem 0.75rem;
      background: #0f0f0f;
      border: 1px solid #333;
      border-radius: 6px;
      color: #e0e0e0;
      font-size: 0.95rem;
      margin-bottom: 1.25rem;
      outline: none;
    }
    input:focus { border-color: #555; }
    button {
      width: 100%;
      padding: 0.65rem;
      background: #2563eb;
      color: #fff;
      border: none;
      border-radius: 6px;
      font-size: 0.95rem;
      cursor: pointer;
    }
    button:hover { background: #1d4ed8; }
  </style>
</head>
<body>
  <div class="card">
    <h1>Apollo Credentials</h1>
    <p>These will be saved locally so Uplink can sync your library.</p>
    <form method="POST" action="/save">
      <label for="username">Username</label>
      <input id="username" name="username" type="text" autocomplete="username" required>
      <label for="password">Password</label>
      <input id="password" name="password" type="password" autocomplete="current-password" required>
      <button type="submit">Save &amp; Continue</button>
    </form>
  </div>
</body>
</html>`

const successHTML = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>Uplink RGL - Saved</title>
  <style>
    *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
    body {
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: #0f0f0f;
      color: #e0e0e0;
      display: flex;
      align-items: center;
      justify-content: center;
      min-height: 100vh;
    }
    .card {
      background: #1a1a1a;
      border: 1px solid #2e2e2e;
      border-radius: 10px;
      padding: 2.5rem 2rem;
      width: 100%;
      max-width: 380px;
      text-align: center;
    }
    h1 { font-size: 1.25rem; margin-bottom: 0.75rem; color: #4ade80; }
    p  { font-size: 0.875rem; color: #888; }
  </style>
</head>
<body>
  <div class="card">
    <h1>Credentials saved</h1>
    <p>You can close this tab. Uplink RGL is continuing in the background.</p>
  </div>
</body>
</html>`
