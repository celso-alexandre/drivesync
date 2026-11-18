//go:build darwin

package main

import (
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

var caseInsensitiveNames = true

func usesLauncher() bool {
	return true
}

func changeTime(info fs.FileInfo) time.Time {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(stat.Ctimespec.Sec, stat.Ctimespec.Nsec)
	}
	return info.ModTime()
}

func openBrowser(link string) {
	_ = exec.Command("/usr/bin/open", link).Start()
}

func baseStateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "drivesync"), nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "drivesync", "state"), nil
}
