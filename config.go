package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

type Config struct {
	Root            string   `json:"root"`
	Folders         []string `json:"folders"`
	IntervalSeconds int      `json:"interval_seconds"`
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "drivesync", "config.json"), nil
}

func loadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	cfg := &Config{Root: filepath.Join(home, "GoogleDrive"), IntervalSeconds: 30}
	path, err := configPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if cfg.IntervalSeconds < 10 {
		cfg.IntervalSeconds = 10
	}
	return cfg, nil
}

func saveConfig(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

func writeFileAtomic(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func changeSelection(names []string, add bool) error {
	if len(names) == 0 {
		return errors.New("name at least one folder")
	}
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := enterSandbox(sandboxPolicy{ReadWrite: []string{filepath.Dir(path)}}); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	for _, name := range names {
		if name == "" || strings.Contains(name, "/") {
			return fmt.Errorf("%q is not a top-level folder name", name)
		}
		index := slices.Index(cfg.Folders, name)
		if add && index < 0 {
			cfg.Folders = append(cfg.Folders, name)
		}
		if !add && index >= 0 {
			cfg.Folders = slices.Delete(cfg.Folders, index, index+1)
		}
	}
	if err := saveConfig(cfg); err != nil {
		return err
	}
	if !add {
		fmt.Println("Stopped syncing. Local copies were kept in", cfg.Root)
	}
	fmt.Println("Selected folders:", strings.Join(cfg.Folders, ", "))
	return nil
}

func listFolders(ctx context.Context) error {
	drive, err := newDrive()
	if err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	policy, err := foldersPolicy()
	if err != nil {
		return err
	}
	if err := enterSandbox(policy); err != nil {
		return err
	}
	folders, err := drive.ListFolders(ctx, "root")
	if err != nil {
		return err
	}
	slices.SortFunc(folders, func(a, b File) int { return strings.Compare(a.Name, b.Name) })
	for _, folder := range folders {
		marker := " "
		if slices.Contains(cfg.Folders, folder.Name) {
			marker = "*"
		}
		fmt.Printf("%s %s\n", marker, folder.Name)
	}
	fmt.Println("\n* = selected. Change with: drivesync select NAME / drivesync unselect NAME")
	return nil
}

func status() error {
	path, err := configPath()
	if err != nil {
		return err
	}
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := enterSandbox(sandboxPolicy{Read: []string{filepath.Dir(path), dir, "/usr/share/zoneinfo", "/etc/localtime"}}); err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	state, _, err := loadState()
	if err != nil {
		return err
	}
	fmt.Println("Local folder:", cfg.Root)
	fmt.Printf("Interval:     %ds\n", cfg.IntervalSeconds)
	if len(cfg.Folders) == 0 {
		fmt.Println("No folders selected. Run: drivesync folders")
		return nil
	}
	for _, name := range cfg.Folders {
		files, folders := 0, 0
		for path, entry := range state.Entries {
			if path == name || !under(path, name) {
				continue
			}
			if entry.Folder {
				folders++
			} else {
				files++
			}
		}
		fmt.Printf("  %s: %d files, %d folders synced\n", name, files, folders)
	}
	return nil
}
