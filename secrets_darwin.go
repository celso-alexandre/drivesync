//go:build darwin

package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

const securityTool = "/usr/bin/security"

func lookupCredentials(entry string) (*credentials, error) {
	out, err := exec.Command(securityTool, "find-generic-password", "-s", keyringService, "-a", entry, "-w").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, errNotSignedIn
		}
		return nil, fmt.Errorf("reading the Keychain: %w", err)
	}
	return parseCredentials(string(out))
}

type pendingSecret struct{}

func prepareCredentialStore() (*pendingSecret, error) {
	return &pendingSecret{}, nil
}

func (p *pendingSecret) commit(creds credentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	cmd := exec.Command(securityTool, "-i")
	cmd.Stdin = strings.NewReader(fmt.Sprintf("add-generic-password -U -s %s -a %s -l drivesync -X %s\n", keyringService, keyringEntry(accountID), hex.EncodeToString(data)))
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("saving to the Keychain: %v: %s", err, strings.TrimSpace(string(out)))
	}
	if _, err := loadStoredCredentials(); err != nil {
		return fmt.Errorf("saving to the Keychain did not stick: %w", err)
	}
	return nil
}

func (p *pendingSecret) abort() {}

func clearCredentials() error {
	out, err := exec.Command(securityTool, "delete-generic-password", "-s", keyringService, "-a", keyringEntry(accountID)).CombinedOutput()
	if err != nil && !strings.Contains(string(out), "could not be found") {
		return fmt.Errorf("clearing the Keychain: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
