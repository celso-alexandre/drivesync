//go:build darwin || windows

package main

import (
	"errors"
	"os"
	"os/exec"
)

func applySandbox(sandboxPolicy) error {
	if !inSandboxChild() {
		return nil
	}
	return verifyConfined()
}

func verifyConfined() error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	if _, err := os.ReadDir(home); err == nil {
		return errors.New("the sandboxed worker can still read the home folder")
	}
	if err := exec.Command(probeProgram[0], probeProgram[1:]...).Run(); err == nil {
		return errors.New("the sandboxed worker can still start programs")
	}
	return nil
}
