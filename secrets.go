package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"strings"
)

type credentials struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

const keyringService = "drivesync"

var accountID string

var accountIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

func keyringEntry(account string) string {
	if account == "" {
		return "credentials"
	}
	return "credentials:" + account
}

func loadStoredCredentials() (*credentials, error) {
	return lookupCredentials(keyringEntry(accountID))
}

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
