//go:build windows

package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

var caseInsensitiveNames = true

func usesLauncher() bool {
	return true
}

func changeTime(info fs.FileInfo) time.Time {
	return info.ModTime()
}

func openBrowser(link string) {
	_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", link).Start()
}

func stateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "drivesync"), nil
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "drivesync", "state"), nil
}
