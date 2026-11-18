package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
)

type credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

const (
	keyringService = "drivesync"
	keyringAccount = "credentials"
)

var errNotSignedIn = errors.New("not signed in; run: drivesync login")

func parseCredentials(data string) (*credentials, error) {
	var creds credentials
	if err := json.Unmarshal([]byte(strings.TrimSpace(data)), &creds); err != nil {
		return nil, errNotSignedIn
	}
	if creds.ClientID == "" || creds.ClientSecret == "" || creds.RefreshToken == "" {
		return nil, errNotSignedIn
	}
	return &creds, nil
}

func loadCredentials() (*credentials, error) {
	if inSandboxChild() {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil {
			return nil, errors.New("sandboxed worker received no credentials")
		}
		return parseCredentials(line)
	}
	return loadStoredCredentials()
}

func writeCredentials(w io.WriteCloser, creds *credentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(data, '\n')); err != nil {
		return err
	}
	return w.Close()
}
