//go:build linux

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
)

var secretAttributes = []string{"application", keyringService, "key", keyringAccount}

func loadStoredCredentials() (*credentials, error) {
	out, err := exec.Command("secret-tool", append([]string{"lookup"}, secretAttributes...)...).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, errNotSignedIn
		}
		return nil, fmt.Errorf("reading the keyring with secret-tool: %w", err)
	}
	return parseCredentials(string(out))
}

type pendingSecret struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

func prepareCredentialStore() (*pendingSecret, error) {
	cmd := exec.Command("secret-tool", append([]string{"store", "--label=drivesync"}, secretAttributes...)...)
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting secret-tool: %w", err)
	}
	return &pendingSecret{cmd: cmd, stdin: stdin}, nil
}

func (p *pendingSecret) commit(creds credentials) error {
	data, err := json.Marshal(creds)
	if err != nil {
		p.abort()
		return err
	}
	if _, err := p.stdin.Write(data); err != nil {
		p.abort()
		return err
	}
	if err := p.stdin.Close(); err != nil {
		p.abort()
		return err
	}
	if err := p.cmd.Wait(); err != nil {
		return fmt.Errorf("saving to the keyring: %w", err)
	}
	return nil
}

func (p *pendingSecret) abort() {
	_ = p.cmd.Process.Kill()
	_ = p.cmd.Wait()
}

func clearCredentials() error {
	cmd := exec.Command("secret-tool", append([]string{"clear"}, secretAttributes...)...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("starting secret-tool: %w", err)
	}
	if err := enterSandbox(sandboxPolicy{AllowSignals: true}); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("clearing the keyring: %w", err)
	}
	return nil
}
