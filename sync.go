package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

const (
	tempPrefix         = ".drivesync-"
	googleAppsPrefix   = "application/vnd.google-apps."
	massDeleteMinimum  = 20
	conflictTimeFormat = "2006-01-02 150405"
)

type Entry struct {
	ID            string    `json:"id"`
	Folder        bool      `json:"folder,omitempty"`
	MD5           string    `json:"md5,omitempty"`
	Size          int64     `json:"size,omitempty"`
	LocalModified time.Time `json:"local_modified,omitzero"`
	LocalChanged  time.Time `json:"local_changed,omitzero"`
}

type State struct {
	PageToken string            `json:"page_token"`
	Entries   map[string]*Entry `json:"entries"`
}

type localItem struct {
	Dir      bool
	Size     int64
	Modified time.Time
	Changed  time.Time
}

func toLocalItem(info fs.FileInfo) localItem {
	item := localItem{Dir: info.IsDir(), Size: info.Size(), Modified: info.ModTime()}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		item.Changed = time.Unix(stat.Ctim.Sec, stat.Ctim.Nsec)
	}
	return item
}

var errRootChanged = errors.New("the local folder setting changed; restarting is required")

type action int

const (
	actionDownload action = iota + 1
	actionUpload
	actionConflict
	actionRecord
	actionTrashRemote
	actionTrashLocal
	actionForget
)

type step struct {
	path   string
	action action
}

type syncer struct {
	drive       *Drive
	cfg         *Config
	state       *State
	statePath   string
	allowDelete bool
	rootID      string
	sandboxRoot string
	remoteFiles []File
	remote      map[string]*File
	local       map[string]localItem
	children    map[string][]*File
	folderIDs   map[string]string
	trashedIDs  map[string]bool
	warned      map[string]bool
}

func loadState() (*State, string, error) {
	dir, err := stateDir()
	if err != nil {
		return nil, "", err
	}
	statePath := filepath.Join(dir, "state.json")
	state := &State{Entries: map[string]*Entry{}}
	data, err := os.ReadFile(statePath)
	if errors.Is(err, fs.ErrNotExist) {
		return state, statePath, nil
	}
	if err != nil {
		return nil, "", err
	}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, "", fmt.Errorf("%s: %w", statePath, err)
	}
	if state.Entries == nil {
		state.Entries = map[string]*Entry{}
	}
	return state, statePath, nil
}

func startSync(ctx context.Context, loop, allowDelete bool) error {
	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	lock, err := os.OpenFile(filepath.Join(dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer lock.Close()
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another drivesync sync is already running")
	}

	drive, err := newDrive()
	if err != nil {
		return err
	}
	state, statePath, err := loadState()
	if err != nil {
		return err
	}
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	cfgPath, err := configPath()
	if err != nil {
		return err
	}
	for _, path := range []string{cfg.Root, filepath.Dir(cfgPath)} {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
	}
	policy := sandboxPolicy{
		ReadWrite:    []string{cfg.Root, dir},
		Read:         append(systemReadPaths(), filepath.Dir(cfgPath)),
		ConnectPorts: internetPorts,
	}
	if err := enterSandbox(policy); err != nil {
		return err
	}
	s := &syncer{drive: drive, state: state, statePath: statePath, allowDelete: allowDelete, sandboxRoot: cfg.Root, warned: map[string]bool{}}
	for {
		err := s.cycle(ctx)
		if ctx.Err() != nil {
			return nil
		}
		if errors.Is(err, errRootChanged) {
			return err
		}
		if err != nil {
			if !loop {
				return err
			}
			log.Printf("sync failed: %v", err)
		}
		if !loop {
			return nil
		}
		interval := 30 * time.Second
		if s.cfg != nil {
			interval = time.Duration(s.cfg.IntervalSeconds) * time.Second
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(interval):
		}
	}
}

func (s *syncer) warnOnce(message string) {
	if !s.warned[message] {
		s.warned[message] = true
		log.Print(message)
	}
}

func (s *syncer) cycle(ctx context.Context) error {
	cfg, err := loadConfig()
	if err != nil {
		return err
	}
	s.cfg = cfg
	if cfg.Root != s.sandboxRoot {
		return errRootChanged
	}
	for p := range s.state.Entries {
		if !slices.Contains(cfg.Folders, topLevel(p)) {
			delete(s.state.Entries, p)
		}
	}
	if len(cfg.Folders) == 0 {
		s.warnOnce("no folders selected; run: drivesync folders, then drivesync select NAME")
		return s.saveState()
	}
	if s.rootID == "" {
		if s.rootID, err = s.drive.RootID(ctx); err != nil {
			return err
		}
	}
	if err := s.refreshRemote(ctx); err != nil {
		return err
	}
	if err := os.MkdirAll(cfg.Root, 0o755); err != nil {
		return err
	}
	s.buildRemote()
	s.trashedIDs = map[string]bool{}
	for _, name := range cfg.Folders {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := s.syncFolder(ctx, name); err != nil {
			log.Printf("%s: %v", name, err)
		}
		if err := s.saveState(); err != nil {
			return err
		}
	}
	return nil
}

func (s *syncer) saveState() error {
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	return writeFileAtomic(s.statePath, data, 0o600)
}

func (s *syncer) refreshRemote(ctx context.Context) error {
	var next string
	if s.remoteFiles != nil {
		changed, token, err := s.drive.Changes(ctx, s.state.PageToken)
		if err != nil {
			return err
		}
		if !changed {
			s.state.PageToken = token
			return nil
		}
		next = token
	} else {
		token, err := s.drive.StartPageToken(ctx)
		if err != nil {
			return err
		}
		next = token
	}
	files, err := s.drive.ListAll(ctx)
	if err != nil {
		return err
	}
	s.remoteFiles = files
	s.state.PageToken = next
	return nil
}

func (s *syncer) buildRemote() {
	s.children = map[string][]*File{}
	for i := range s.remoteFiles {
		file := &s.remoteFiles[i]
		for _, parent := range file.Parents {
			s.children[parent] = append(s.children[parent], file)
		}
	}
	for _, list := range s.children {
		slices.SortFunc(list, func(a, b *File) int {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return strings.Compare(a.ID, b.ID)
		})
	}
	s.remote = map[string]*File{}
	s.folderIDs = map[string]string{}
	var walk func(id, prefix string)
	walk = func(id, prefix string) {
		for _, file := range s.children[id] {
			if !validName(file.Name) {
				s.warnOnce(fmt.Sprintf("skipped Drive item with an unusable name: %s/%q", prefix, file.Name))
				continue
			}
			if !file.IsFolder() && strings.HasPrefix(file.MimeType, googleAppsPrefix) {
				continue
			}
			p := prefix + "/" + file.Name
			if _, duplicate := s.remote[p]; duplicate {
				s.warnOnce("skipped duplicate name in Drive: " + p)
				continue
			}
			s.remote[p] = file
			if file.IsFolder() {
				s.folderIDs[p] = file.ID
				walk(file.ID, p)
			}
		}
	}
	for _, name := range s.cfg.Folders {
		for _, file := range s.children[s.rootID] {
			if file.Name != name || !file.IsFolder() {
				continue
			}
			if _, duplicate := s.remote[name]; duplicate {
				s.warnOnce("several top-level folders are named " + name + "; syncing the first one")
				continue
			}
			s.remote[name] = file
			s.folderIDs[name] = file.ID
			walk(file.ID, name)
		}
	}
}

func validName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.Contains(name, "/") && !strings.HasPrefix(name, tempPrefix)
}

func topLevel(p string) string {
	top, _, _ := strings.Cut(p, "/")
	return top
}

func under(p, root string) bool {
	return p == root || strings.HasPrefix(p, root+"/")
}

func (s *syncer) localPath(p string) string {
	return filepath.Join(s.cfg.Root, filepath.FromSlash(p))
}

func (s *syncer) scanLocal(name string) (map[string]localItem, error) {
	items := map[string]localItem{}
	base := s.localPath(name)
	err := filepath.WalkDir(base, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if full == base {
			return nil
		}
		if strings.HasPrefix(entry.Name(), tempPrefix) {
			if entry.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(s.cfg.Root, full)
		if err != nil {
			return err
		}
		items[filepath.ToSlash(rel)] = toLocalItem(info)
		return nil
	})
	return items, err
}

func (s *syncer) syncFolder(ctx context.Context, name string) error {
	known := 0
	for p := range s.state.Entries {
		if under(p, name) && p != name {
			known++
		}
	}
	rootFolder := s.remote[name]
	if rootFolder == nil {
		if known > 0 {
			return errors.New("folder is gone from My Drive; restore it from the Drive trash or unselect it (local copy is untouched)")
		}
		return errors.New("no top-level folder with this name in My Drive")
	}
	base := s.localPath(name)
	info, err := os.Stat(base)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if known > 0 {
			return fmt.Errorf("%s is missing locally; restore it or unselect the folder (Drive is untouched)", base)
		}
		if err := os.MkdirAll(base, 0o755); err != nil {
			return err
		}
	case err != nil:
		return err
	case !info.IsDir():
		return fmt.Errorf("%s is not a directory", base)
	}
	s.state.Entries[name] = &Entry{ID: rootFolder.ID, Folder: true}

	local, err := s.scanLocal(name)
	if err != nil {
		return fmt.Errorf("scanning local files: %w", err)
	}
	s.local = local

	paths := map[string]bool{}
	for p := range s.remote {
		if under(p, name) && p != name {
			paths[p] = true
		}
	}
	for p := range local {
		paths[p] = true
	}
	for p := range s.state.Entries {
		if under(p, name) && p != name {
			paths[p] = true
		}
	}
	sorted := make([]string, 0, len(paths))
	for p := range paths {
		sorted = append(sorted, p)
	}
	slices.Sort(sorted)

	var steps []step
	var folders []string
	deletions := 0
	for _, p := range sorted {
		remote := s.remote[p]
		item, hasLocal := local[p]
		entry := s.state.Entries[p]
		remoteDir := remote != nil && remote.IsFolder()
		if remote != nil && hasLocal && remoteDir != item.Dir {
			s.warnOnce("skipped, file on one side and folder on the other: " + p)
			continue
		}
		if remoteDir || (hasLocal && item.Dir) || (remote == nil && !hasLocal && entry != nil && entry.Folder) {
			folders = append(folders, p)
			continue
		}
		act, err := s.decide(p, remote, item, hasLocal, entry)
		if err != nil {
			log.Printf("%s: %v", p, err)
			continue
		}
		if act == actionTrashRemote || act == actionTrashLocal {
			deletions++
		}
		if act != 0 {
			steps = append(steps, step{path: p, action: act})
		}
	}

	blockDeletes := !s.allowDelete && deletions > massDeleteMinimum && deletions*2 > known
	if blockDeletes {
		log.Printf("%s: refusing to delete %d of %d items in one pass; if intended, run: drivesync sync --allow-delete", name, deletions, known)
	}
	for _, st := range steps {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if blockDeletes && (st.action == actionTrashRemote || st.action == actionTrashLocal) {
			continue
		}
		if err := s.apply(ctx, st); err != nil {
			log.Printf("%s: %v", st.path, err)
		}
	}
	return s.syncFolders(ctx, folders, blockDeletes)
}

func (s *syncer) decide(p string, remote *File, item localItem, hasLocal bool, entry *Entry) (action, error) {
	if entry != nil {
		remoteChanged := remote == nil || remote.ID != entry.ID || remote.MD5 != entry.MD5
		localChanged, err := s.localChanged(p, item, hasLocal, entry)
		if err != nil {
			return 0, err
		}
		switch {
		case remote != nil && hasLocal:
			switch {
			case !remoteChanged && !localChanged:
				if !item.Changed.Equal(entry.LocalChanged) {
					return actionRecord, nil
				}
				return 0, nil
			case remoteChanged && !localChanged:
				return actionDownload, nil
			case localChanged && !remoteChanged:
				return actionUpload, nil
			}
			return s.compareContent(p, remote)
		case remote == nil && hasLocal:
			if localChanged {
				return actionUpload, nil
			}
			return actionTrashLocal, nil
		case remote != nil && !hasLocal:
			if remoteChanged {
				return actionDownload, nil
			}
			return actionTrashRemote, nil
		}
		return actionForget, nil
	}
	switch {
	case remote != nil && hasLocal:
		return s.compareContent(p, remote)
	case remote != nil:
		return actionDownload, nil
	case hasLocal:
		return actionUpload, nil
	}
	return 0, nil
}

func (s *syncer) localChanged(p string, item localItem, hasLocal bool, entry *Entry) (bool, error) {
	if !hasLocal || item.Size != entry.Size || !item.Modified.Equal(entry.LocalModified) {
		return true, nil
	}
	if item.Changed.Equal(entry.LocalChanged) {
		return false, nil
	}
	sum, err := fileMD5(s.localPath(p))
	if err != nil {
		return false, err
	}
	return sum != entry.MD5, nil
}

func (s *syncer) unchangedSinceScan(p string) error {
	info, err := os.Lstat(s.localPath(p))
	scanned, wasScanned := s.local[p]
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !wasScanned {
		return errors.New("appeared locally during this pass; will retry")
	}
	now := toLocalItem(info)
	if now.Size != scanned.Size || !now.Modified.Equal(scanned.Modified) || !now.Changed.Equal(scanned.Changed) {
		return errors.New("changed locally during this pass; will retry")
	}
	return nil
}

func (s *syncer) compareContent(p string, remote *File) (action, error) {
	sum, err := fileMD5(s.localPath(p))
	if err != nil {
		return 0, err
	}
	if sum == remote.MD5 {
		return actionRecord, nil
	}
	return actionConflict, nil
}

func fileMD5(full string) (string, error) {
	file, err := os.Open(full)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func (s *syncer) apply(ctx context.Context, st step) error {
	p := st.path
	remote := s.remote[p]
	switch st.action {
	case actionDownload:
		return s.download(ctx, p, remote)
	case actionUpload:
		id := ""
		if remote != nil {
			id = remote.ID
		}
		return s.upload(ctx, p, id)
	case actionConflict:
		if err := s.unchangedSinceScan(p); err != nil {
			return err
		}
		full := s.localPath(p)
		copyPath := conflictPath(full)
		if err := os.Rename(full, copyPath); err != nil {
			return err
		}
		log.Printf("%s: changed on both sides; kept your version as %s", p, filepath.Base(copyPath))
		return s.download(ctx, p, remote)
	case actionRecord:
		return s.record(p, remote)
	case actionTrashRemote:
		if err := s.drive.Trash(ctx, s.state.Entries[p].ID); err != nil {
			return err
		}
		s.trashedIDs[s.state.Entries[p].ID] = true
		delete(s.state.Entries, p)
		delete(s.remote, p)
		log.Printf("%s: deleted locally, moved to Drive trash", p)
	case actionTrashLocal:
		if err := s.unchangedSinceScan(p); err != nil {
			return err
		}
		if err := s.moveToLocalTrash(p); err != nil {
			return err
		}
		delete(s.state.Entries, p)
		log.Printf("%s: deleted in Drive, moved to %s", p, filepath.Join(s.cfg.Root, tempPrefix+"trash"))
	case actionForget:
		delete(s.state.Entries, p)
	}
	return nil
}

func (s *syncer) download(ctx context.Context, p string, remote *File) error {
	full := s.localPath(p)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return err
	}
	if err := s.drive.Download(ctx, remote.ID, full, func() error { return s.unchangedSinceScan(p) }); err != nil {
		return err
	}
	if err := os.Chtimes(full, time.Now(), remote.ModifiedTime); err != nil {
		return err
	}
	log.Printf("%s: downloaded", p)
	return s.record(p, remote)
}

func (s *syncer) record(p string, remote *File) error {
	info, err := os.Stat(s.localPath(p))
	if err != nil {
		return err
	}
	item := toLocalItem(info)
	s.state.Entries[p] = &Entry{ID: remote.ID, MD5: remote.MD5, Size: item.Size, LocalModified: item.Modified, LocalChanged: item.Changed}
	return nil
}

func (s *syncer) upload(ctx context.Context, p, id string) error {
	full := s.localPath(p)
	info, err := os.Stat(full)
	if err != nil {
		return err
	}
	parentID, err := s.ensureRemoteFolder(ctx, path.Dir(p))
	if err != nil {
		return err
	}
	uploaded, err := s.drive.Upload(ctx, id, path.Base(p), parentID, full, info.ModTime())
	if err != nil {
		return err
	}
	s.remote[p] = uploaded
	item := toLocalItem(info)
	s.state.Entries[p] = &Entry{ID: uploaded.ID, MD5: uploaded.MD5, Size: item.Size, LocalModified: item.Modified, LocalChanged: item.Changed}
	log.Printf("%s: uploaded", p)
	return nil
}

func (s *syncer) ensureRemoteFolder(ctx context.Context, p string) (string, error) {
	if id, ok := s.folderIDs[p]; ok {
		return id, nil
	}
	if !strings.Contains(p, "/") {
		return "", fmt.Errorf("top-level folder %s is not in Drive", p)
	}
	parentID, err := s.ensureRemoteFolder(ctx, path.Dir(p))
	if err != nil {
		return "", err
	}
	folder, err := s.drive.CreateFolder(ctx, path.Base(p), parentID)
	if err != nil {
		return "", err
	}
	s.folderIDs[p] = folder.ID
	s.remote[p] = folder
	s.state.Entries[p] = &Entry{ID: folder.ID, Folder: true}
	log.Printf("%s: folder created in Drive", p)
	return folder.ID, nil
}

func (s *syncer) syncFolders(ctx context.Context, folders []string, blockDeletes bool) error {
	var removeRemote, removeLocal []string
	for _, p := range folders {
		remote := s.remote[p]
		_, err := os.Stat(s.localPath(p))
		localExists := err == nil
		entry := s.state.Entries[p]
		switch {
		case remote != nil && localExists:
			s.state.Entries[p] = &Entry{ID: remote.ID, Folder: true}
		case remote != nil && entry == nil:
			if err := os.MkdirAll(s.localPath(p), 0o755); err != nil {
				log.Printf("%s: %v", p, err)
				continue
			}
			s.state.Entries[p] = &Entry{ID: remote.ID, Folder: true}
		case remote != nil:
			removeRemote = append(removeRemote, p)
		case localExists && entry == nil:
			if _, err := s.ensureRemoteFolder(ctx, p); err != nil {
				log.Printf("%s: %v", p, err)
			}
		case localExists:
			removeLocal = append(removeLocal, p)
		default:
			delete(s.state.Entries, p)
		}
	}
	if blockDeletes {
		return nil
	}
	slices.Reverse(removeRemote)
	for _, p := range removeRemote {
		remote := s.remote[p]
		if s.hasLiveChildren(remote.ID) {
			if err := os.MkdirAll(s.localPath(p), 0o755); err != nil {
				log.Printf("%s: %v", p, err)
			}
			s.warnOnce(p + ": folder still holds Google Docs or skipped items in Drive, so it was not deleted there")
			continue
		}
		if err := s.drive.Trash(ctx, remote.ID); err != nil {
			log.Printf("%s: %v", p, err)
			continue
		}
		s.trashedIDs[remote.ID] = true
		delete(s.state.Entries, p)
		delete(s.remote, p)
		log.Printf("%s: folder deleted locally, moved to Drive trash", p)
	}
	slices.Reverse(removeLocal)
	for _, p := range removeLocal {
		if err := os.Remove(s.localPath(p)); err != nil {
			log.Printf("%s: folder deleted in Drive but kept locally: %v", p, err)
			continue
		}
		delete(s.state.Entries, p)
		log.Printf("%s: folder deleted in Drive, removed locally", p)
	}
	return nil
}

func (s *syncer) hasLiveChildren(id string) bool {
	for _, child := range s.children[id] {
		if !s.trashedIDs[child.ID] {
			return true
		}
	}
	return false
}

func (s *syncer) moveToLocalTrash(p string) error {
	dest := filepath.Join(s.cfg.Root, tempPrefix+"trash", time.Now().Format("2006-01-02"), filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.Rename(s.localPath(p), dest)
}

func conflictPath(full string) string {
	dir, base := filepath.Split(full)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	return filepath.Join(dir, fmt.Sprintf("%s (conflict %s)%s", stem, time.Now().Format(conflictTimeFormat), ext))
}
