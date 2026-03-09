package main

import (
	"fmt"
	"os"
	"os/exec"
)

const regKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`
const regValue = "Uplink RGL"

func autoStartEnabled() bool {
	err := exec.Command("reg", "query", regKey, "/v", regValue).Run()
	return err == nil
}

func enableAutoStart() {
	exePath, err := os.Executable()
	if err != nil {
		fmt.Println("Auto-start: could not determine executable path:", err)
		return
	}
	err = exec.Command("reg", "add", regKey, "/v", regValue, "/t", "REG_SZ", "/d", exePath, "/f").Run()
	if err != nil {
		fmt.Println("Auto-start: failed to add registry key:", err)
	} else {
		fmt.Println("Auto-start enabled.")
	}
}

func disableAutoStart() {
	err := exec.Command("reg", "delete", regKey, "/v", regValue, "/f").Run()
	if err != nil {
		fmt.Println("Auto-start: failed to remove registry key:", err)
	} else {
		fmt.Println("Auto-start disabled.")
	}
}

func autoStartLabel() string {
	if autoStartEnabled() {
		return "Disable Auto-start"
	}
	return "Enable Auto-start"
}
