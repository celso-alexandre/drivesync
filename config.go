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
	Root            string            `json:"root"`
	Folders         []string          `json:"folders"`
	IntervalSeconds int               `json:"interval_seconds"`
	Paused          bool              `json:"paused,omitempty"`
	SyncRequested   int64             `json:"sync_requested,omitempty"`
	Decisions       map[string]string `json:"decisions,omitempty"`
}

func accountScoped(dir string) string {
	if accountID == "" {
		return dir
	}
	return filepath.Join(dir, "accounts", accountID)
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(accountScoped(filepath.Join(dir, "drivesync")), "config.json"), nil
}

func stateDir() (string, error) {
	dir, err := baseStateDir()
	if err != nil {
		return "", err
	}
	return accountScoped(dir), nil
}

func loadConfig() (*Config, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	root := filepath.Join(home, "GoogleDrive")
	if accountID != "" {
		root = filepath.Join(root, accountID)
	}
	cfg := &Config{Root: root, IntervalSeconds: 30}
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

func listFolders(ctx context.Context, asJSON, withSizes bool) error {
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
	if asJSON {
		type folderChoice struct {
			Name     string `json:"name"`
			Selected bool   `json:"selected"`
			Files    *int   `json:"files,omitempty"`
			Bytes    *int64 `json:"bytes,omitempty"`
		}
		var sizes map[string]folderSize
		if withSizes {
			all, err := drive.ListAll(ctx)
			if err != nil {
				return err
			}
			sizes = folderSizes(all, folders)
		}
		choices := []folderChoice{}
		for _, folder := range folders {
			choice := folderChoice{Name: folder.Name, Selected: slices.Contains(cfg.Folders, folder.Name)}
			if size, ok := sizes[folder.ID]; ok {
				choice.Files, choice.Bytes = &size.files, &size.bytes
			}
			choices = append(choices, choice)
		}
		return json.NewEncoder(os.Stdout).Encode(choices)
	}
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

func printPaths() error {
	cfgPath, err := configPath()
	if err != nil {
		return err
	}
	state, err := stateDir()
	if err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	if err := enterSandbox(sandboxPolicy{}); err != nil {
		return err
	}
	return json.NewEncoder(os.Stdout).Encode(map[string]string{
		"config": cfgPath,
		"state":  state,
		"status": filepath.Join(state, "status.json"),
		"root":   cfg.Root,
	})
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

type folderSize struct {
	files int
	bytes int64
}

func folderSizes(all []File, tops []File) map[string]folderSize {
	children := map[string][]*File{}
	for i := range all {
		for _, parent := range all[i].Parents {
			children[parent] = append(children[parent], &all[i])
		}
	}
	sizes := map[string]folderSize{}
	for _, top := range tops {
		var size folderSize
		seen := map[string]bool{top.ID: true}
		queue := []string{top.ID}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			for _, child := range children[id] {
				if seen[child.ID] {
					continue
				}
				seen[child.ID] = true
				switch {
				case child.IsFolder():
					queue = append(queue, child.ID)
				case !strings.HasPrefix(child.MimeType, googleAppsPrefix):
					size.files++
					size.bytes += child.Size
				}
			}
		}
		sizes[top.ID] = size
	}
	return sizes
}
