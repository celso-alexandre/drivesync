package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func withAccount(t *testing.T, id string) {
	t.Helper()
	old := accountID
	accountID = id
	t.Cleanup(func() { accountID = old })
}

func TestAccountScopesEveryLocation(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	withAccount(t, "a1b2")
	cfgPath, _ := configPath()
	state, _ := stateDir()
	cfg, _ := loadConfig()
	if !strings.HasSuffix(cfgPath, filepath.Join("drivesync", "accounts", "a1b2", "config.json")) {
		t.Fatalf("config not scoped: %s", cfgPath)
	}
	if !strings.HasSuffix(state, filepath.Join("drivesync", "accounts", "a1b2")) {
		t.Fatalf("state not scoped: %s", state)
	}
	if cfg.Root != filepath.Join(home, "GoogleDrive", "a1b2") {
		t.Fatalf("default folder not scoped: %s", cfg.Root)
	}
	if keyringEntry(accountID) != "credentials:a1b2" || keyringEntry("") != "credentials" {
		t.Fatal("keyring entries are not separate per account")
	}
}

func TestTwoAccountsDoNotShareState(t *testing.T) {
	home := t.TempDir()
	withAccount(t, "first")
	first := newHarnessIn(t, home)
	work := first.drive.add("Work", "root-id", nil, true)
	first.drive.add("a.txt", work, []byte("first account"), false)
	first.run()
	firstState := first.sync.statePath

	accountID = "second"
	second := newHarnessIn(t, home)
	if second.sync.statePath == firstState {
		t.Fatal("both accounts use the same state file")
	}
	if len(second.sync.state.Entries) != 0 {
		t.Fatal("the second account sees the first account's sync state")
	}
	other := second.drive.add("Work", "root-id", nil, true)
	second.drive.add("b.txt", other, []byte("second account"), false)
	second.run()
	if _, err := os.Stat(filepath.Join(first.root, "Work", "b.txt")); err == nil {
		t.Fatal("second account wrote into the first account's folder")
	}
	if first.root == second.root {
		t.Fatal("accounts share a sync folder")
	}
}

func TestAccountIDsAreValidated(t *testing.T) {
	for _, id := range []string{"abc", "a1-b2", "0"} {
		if !accountIDPattern.MatchString(id) {
			t.Errorf("%q rejected", id)
		}
	}
	for _, id := range []string{"", "../x", "A", "a/b", "-a", strings.Repeat("a", 41)} {
		if accountIDPattern.MatchString(id) {
			t.Errorf("%q accepted", id)
		}
	}
}
