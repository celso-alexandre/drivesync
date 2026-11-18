package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

type sandboxPolicy struct {
	ReadWrite    []string
	Read         []string
	ConnectPorts []uint16
	AllowSignals bool
}

const sandboxChildEnv = "DRIVESYNC_SANDBOX_CHILD"

var sandboxDisabled bool

var internetPorts = []uint16{443, 53}

type exitCodeError int

func (e exitCodeError) Error() string {
	return fmt.Sprintf("sandboxed worker exited with status %d", int(e))
}

func inSandboxChild() bool {
	return usesLauncher() && os.Getenv(sandboxChildEnv) == "1"
}

func enterSandbox(policy sandboxPolicy) error {
	if sandboxDisabled {
		fmt.Fprintln(os.Stderr, "drivesync: WARNING: running without the sandbox")
		return nil
	}
	if err := applySandbox(policy); err != nil {
		return fmt.Errorf("cannot sandbox drivesync: %w (use --unsafe-no-sandbox to run without it)", err)
	}
	return nil
}

func maybeLaunch(ctx context.Context, args []string, policy func() (sandboxPolicy, error)) (bool, error) {
	if !usesLauncher() || inSandboxChild() || sandboxDisabled {
		return false, nil
	}
	p, err := policy()
	if err != nil {
		return true, err
	}
	creds, err := loadCredentials()
	if err != nil {
		return true, err
	}
	if accountID != "" {
		args = append([]string{"--account", accountID}, args...)
	}
	if err := launchSandboxed(ctx, args, p, creds); err != nil {
		return true, err
	}
	return true, nil
}

func systemReadPaths() []string {
	if runtime.GOOS != "linux" {
		return nil
	}
	paths := []string{"/etc", "/usr"}
	if target, err := filepath.EvalSymlinks("/etc/resolv.conf"); err == nil {
		paths = append(paths, filepath.Dir(target))
	}
	return paths
}

func syncPolicy() (sandboxPolicy, error) {
	cfg, err := loadConfig()
	if err != nil {
		return sandboxPolicy{}, err
	}
	cfgPath, err := configPath()
	if err != nil {
		return sandboxPolicy{}, err
	}
	state, err := stateDir()
	if err != nil {
		return sandboxPolicy{}, err
	}
	for _, path := range []string{cfg.Root, filepath.Dir(cfgPath), state} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return sandboxPolicy{}, err
		}
	}
	return sandboxPolicy{
		ReadWrite:    []string{cfg.Root, state},
		Read:         append(systemReadPaths(), filepath.Dir(cfgPath)),
		ConnectPorts: internetPorts,
	}, nil
}

func foldersPolicy() (sandboxPolicy, error) {
	cfgPath, err := configPath()
	if err != nil {
		return sandboxPolicy{}, err
	}
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o700); err != nil {
		return sandboxPolicy{}, err
	}
	return sandboxPolicy{Read: append(systemReadPaths(), filepath.Dir(cfgPath)), ConnectPorts: internetPorts}, nil
}
