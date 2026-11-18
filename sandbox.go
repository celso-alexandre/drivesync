package main

import (
	"fmt"
	"os"
	"path/filepath"
)

type sandboxPolicy struct {
	ReadWrite    []string
	Read         []string
	ConnectPorts []uint16
	AllowSignals bool
}

var sandboxDisabled bool

var internetPorts = []uint16{443, 53}

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

func systemReadPaths() []string {
	paths := []string{"/etc", "/usr"}
	if target, err := filepath.EvalSymlinks("/etc/resolv.conf"); err == nil {
		paths = append(paths, filepath.Dir(target))
	}
	return paths
}
