//go:build linux

package main

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

var caseInsensitiveNames = false

var probeProgram = []string{"/bin/true"}

func verifyConfined() error {
	return nil
}

func usesLauncher() bool {
	return false
}

func changeTime(info fs.FileInfo) time.Time {
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		return time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
	}
	return info.ModTime()
}

func openBrowser(link string) {
	_ = exec.Command("xdg-open", link).Start()
}

func baseStateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "drivesync"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "drivesync"), nil
}

func launchSandboxed(_ context.Context, _ []string, _ sandboxPolicy, _ *credentials) error {
	return errors.New("Linux sandboxes in-process")
}
