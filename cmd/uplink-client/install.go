package main

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
)

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
