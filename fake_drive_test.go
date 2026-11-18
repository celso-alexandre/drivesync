package main

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeFile struct {
	File
	content   []byte
	revisions [][]byte
	trashed   bool
}

type fakeDrive struct {
	mu       sync.Mutex
	files    map[string]*fakeFile
	nextID   int
	revision int
	failNext map[string]int
	requests int
	server   *httptest.Server
}

func newFakeDrive(t *testing.T) *fakeDrive {
	f := &fakeDrive{files: map[string]*fakeFile{}, failNext: map[string]int{}}
	f.files["root-id"] = &fakeFile{File: File{ID: "root-id", Name: "My Drive", MimeType: folderMime}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.server.Close)
	oldAPI, oldUpload := apiBase, uploadBase
	apiBase = f.server.URL + "/drive/v3"
	uploadBase = f.server.URL + "/upload/drive/v3"
	t.Cleanup(func() { apiBase, uploadBase = oldAPI, oldUpload })
	return f
}

func (f *fakeDrive) add(name, parent string, content []byte, folder bool) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.addLocked(name, parent, content, folder)
}

func (f *fakeDrive) addLocked(name, parent string, content []byte, folder bool) string {
	f.nextID++
	id := fmt.Sprintf("id%d", f.nextID)
	file := &fakeFile{File: File{ID: id, Name: name, Parents: []string{parent}, ModifiedTime: time.Now().UTC().Truncate(time.Millisecond)}}
	if folder {
		file.MimeType = folderMime
	} else {
		file.MimeType = "application/octet-stream"
		f.setContent(file, content)
	}
	f.files[id] = file
	f.revision++
	return id
}

func (f *fakeDrive) addGoogleDoc(name, parent string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := f.addLocked(name, parent, nil, false)
	f.files[id].MimeType = googleAppsPrefix + "document"
	f.files[id].MD5 = ""
	return id
}

func (f *fakeDrive) setContent(file *fakeFile, content []byte) {
	if file.content != nil {
		file.revisions = append(file.revisions, file.content)
	}
	if content == nil {
		content = []byte{}
	}
	sum := md5.Sum(content)
	file.content = content
	file.MD5 = hex.EncodeToString(sum[:])
	file.Size = int64(len(content))
}

func (f *fakeDrive) edit(id string, content []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.setContent(f.files[id], content)
	f.files[id].ModifiedTime = time.Now().UTC().Truncate(time.Millisecond)
	f.revision++
}

func (f *fakeDrive) trash(id string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[id].trashed = true
	f.revision++
}

func (f *fakeDrive) move(id, newParent, newName string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files[id].Parents = []string{newParent}
	f.files[id].Name = newName
	f.revision++
}

func (f *fakeDrive) live(id string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	file := f.files[id]
	for file != nil {
		if file.trashed {
			return false
		}
		if file.ID == "root-id" {
			return true
		}
		file = f.files[file.Parents[0]]
	}
	return false
}

func (f *fakeDrive) find(parent, name string) *fakeFile {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.findLocked(parent, name)
}

func (f *fakeDrive) findLocked(parent, name string) *fakeFile {
	for _, file := range f.files {
		if !file.trashed && file.Name == name && len(file.Parents) > 0 && file.Parents[0] == parent {
			return file
		}
	}
	return nil
}

func (f *fakeDrive) lookup(p string) *fakeFile {
	f.mu.Lock()
	defer f.mu.Unlock()
	parent := "root-id"
	var file *fakeFile
	for _, part := range strings.Split(p, "/") {
		file = f.findLocked(parent, part)
		if file == nil {
			return nil
		}
		parent = file.ID
	}
	return file
}

func (f *fakeDrive) tree(rootID string) (map[string]string, map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	files, folders := map[string]string{}, map[string]bool{}
	var walk func(id, prefix string)
	walk = func(id, prefix string) {
		for _, file := range f.files {
			if file.trashed || len(file.Parents) == 0 || file.Parents[0] != id || file.ID == "root-id" {
				continue
			}
			p := path.Join(prefix, file.Name)
			if file.MimeType == folderMime {
				folders[p] = true
				walk(file.ID, p)
			} else if !strings.HasPrefix(file.MimeType, googleAppsPrefix) {
				files[p] = string(file.content)
			}
		}
	}
	walk(rootID, "")
	return files, folders
}

func (f *fakeDrive) allContents(into map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, file := range f.files {
		if file.content != nil {
			into[string(file.content)] = true
		}
		for _, old := range file.revisions {
			into[string(old)] = true
		}
	}
}

func (f *fakeDrive) failRequests(prefix string, count int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failNext[prefix] = count
}

func (f *fakeDrive) handle(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests++
	p := r.URL.Path
	if r.Header.Get("Authorization") != "Bearer test" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	for prefix, count := range f.failNext {
		if count > 0 && strings.HasPrefix(r.Method+" "+p, prefix) {
			f.failNext[prefix] = count - 1
			http.Error(w, `{"error":"backendError"}`, http.StatusInternalServerError)
			return
		}
	}
	writeJSON := func(v any) { json.NewEncoder(w).Encode(v) }
	switch {
	case p == "/drive/v3/about":
		writeJSON(map[string]any{"user": map[string]string{"emailAddress": "tester@example.com"}})
	case p == "/drive/v3/files/root":
		writeJSON(File{ID: "root-id"})
	case p == "/drive/v3/files" && r.Method == http.MethodGet:
		query := r.URL.Query().Get("q")
		var list []File
		for _, file := range f.files {
			if file.trashed || file.ID == "root-id" {
				continue
			}
			if strings.Contains(query, "'root' in parents") && (file.Parents[0] != "root-id" || file.MimeType != folderMime) {
				continue
			}
			list = append(list, file.File)
		}
		writeJSON(map[string]any{"files": list})
	case p == "/drive/v3/files" && r.Method == http.MethodPost:
		var meta struct {
			Name    string   `json:"name"`
			Parents []string `json:"parents"`
		}
		json.NewDecoder(r.Body).Decode(&meta)
		id := f.addLocked(meta.Name, meta.Parents[0], nil, true)
		writeJSON(f.files[id].File)
	case p == "/drive/v3/changes/startPageToken":
		writeJSON(map[string]string{"startPageToken": fmt.Sprint(f.revision)})
	case p == "/drive/v3/changes":
		var since int
		fmt.Sscan(r.URL.Query().Get("pageToken"), &since)
		var changes []map[string]string
		if f.revision > since {
			changes = append(changes, map[string]string{"fileId": "x"})
		}
		writeJSON(map[string]any{"changes": changes, "newStartPageToken": fmt.Sprint(f.revision)})
	case strings.HasPrefix(p, "/drive/v3/files/") && r.Method == http.MethodGet:
		file := f.files[strings.TrimPrefix(p, "/drive/v3/files/")]
		if file == nil || r.URL.Query().Get("alt") != "media" {
			http.NotFound(w, r)
			return
		}
		w.Write(file.content)
	case strings.HasPrefix(p, "/drive/v3/files/") && r.Method == http.MethodPatch:
		file := f.files[strings.TrimPrefix(p, "/drive/v3/files/")]
		if file == nil {
			http.NotFound(w, r)
			return
		}
		file.trashed = true
		f.revision++
		writeJSON(map[string]string{"id": file.ID})
	case strings.HasPrefix(p, "/upload/drive/v3/files"):
		if r.URL.Query().Get("uploadType") != "resumable" {
			http.Error(w, "resumable only", http.StatusBadRequest)
			return
		}
		var meta struct {
			Name         string    `json:"name"`
			Parents      []string  `json:"parents"`
			ModifiedTime time.Time `json:"modifiedTime"`
		}
		json.NewDecoder(r.Body).Decode(&meta)
		id := strings.TrimPrefix(strings.TrimPrefix(p, "/upload/drive/v3/files"), "/")
		if id == "" {
			parent := f.files[meta.Parents[0]]
			if parent == nil || parent.trashed {
				http.Error(w, "parent not found", http.StatusNotFound)
				return
			}
			id = f.addLocked(meta.Name, meta.Parents[0], nil, false)
		} else if f.files[id] == nil {
			http.NotFound(w, r)
			return
		}
		f.files[id].ModifiedTime = meta.ModifiedTime
		w.Header().Set("Location", f.server.URL+"/session/"+id)
	case strings.HasPrefix(p, "/session/"):
		file := f.files[strings.TrimPrefix(p, "/session/")]
		content, _ := io.ReadAll(r.Body)
		if int64(len(content)) != r.ContentLength {
			http.Error(w, "length mismatch", http.StatusBadRequest)
			return
		}
		f.setContent(file, content)
		f.revision++
		writeJSON(file.File)
	default:
		http.Error(w, "unexpected "+r.Method+" "+p, http.StatusNotImplemented)
	}
}

type harness struct {
	t     *testing.T
	home  string
	drive *fakeDrive
	root  string
	sync  *syncer
	clock time.Time
}

func newHarness(t *testing.T) *harness {
	return newHarnessIn(t, t.TempDir())
}

func newHarnessIn(t *testing.T, home string) *harness {
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(home, ".state"))
	fake := newFakeDrive(t)
	root := filepath.Join(home, "GoogleDrive")
	if accountID != "" {
		root = filepath.Join(root, accountID)
	}
	if err := saveConfig(&Config{Root: root, Folders: []string{"Work"}, IntervalSeconds: 30}); err != nil {
		t.Fatal(err)
	}
	if dir, err := stateDir(); err != nil || os.MkdirAll(dir, 0o700) != nil {
		t.Fatal(err)
	}
	h := &harness{t: t, home: home, drive: fake, root: root, clock: time.Now().Add(-time.Hour).Truncate(time.Second)}
	h.restart()
	return h
}

func (h *harness) restart() {
	h.t.Helper()
	state, statePath, err := loadState()
	if err != nil {
		h.t.Fatal(err)
	}
	tokens := &tokenSource{accessToken: "test", expiry: time.Now().Add(time.Hour)}
	allowDelete := h.sync != nil && h.sync.allowDelete
	h.sync = &syncer{drive: &Drive{tokens: tokens, client: h.drive.server.Client()}, state: state, statePath: statePath, allowDelete: allowDelete, sandboxRoot: h.root, warned: map[string]bool{}}
}

func (h *harness) run() {
	h.t.Helper()
	if err := h.sync.cycle(context.Background()); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) full(rel string) string {
	return filepath.Join(h.root, filepath.FromSlash(rel))
}

func (h *harness) read(rel string) string {
	h.t.Helper()
	data, err := os.ReadFile(h.full(rel))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

func (h *harness) write(rel, content string) {
	h.t.Helper()
	full := h.full(rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		h.t.Fatal(err)
	}
	h.clock = h.clock.Add(time.Second)
	if err := os.Chtimes(full, h.clock, h.clock); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) exists(rel string) bool {
	_, err := os.Stat(h.full(rel))
	return err == nil
}

func (h *harness) localTree(top string) (map[string]string, map[string]bool) {
	h.t.Helper()
	files, folders := map[string]string{}, map[string]bool{}
	base := h.full(top)
	if _, err := os.Stat(base); err != nil {
		return files, folders
	}
	err := filepath.WalkDir(base, func(full string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(h.root, full)
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			folders[rel] = true
			return nil
		}
		data, err := os.ReadFile(full)
		files[rel] = string(data)
		return err
	})
	if err != nil {
		h.t.Fatal(err)
	}
	return files, folders
}

func (h *harness) allContents() map[string]bool {
	h.t.Helper()
	contents := map[string]bool{}
	filepath.WalkDir(h.root, func(full string, entry fs.DirEntry, err error) error {
		if err == nil && entry.Type().IsRegular() {
			data, readErr := os.ReadFile(full)
			if readErr != nil {
				h.t.Fatal(readErr)
			}
			contents[string(data)] = true
		}
		return nil
	})
	h.drive.allContents(contents)
	return contents
}

func (h *harness) assertConverged(top string) {
	h.t.Helper()
	localFiles, localFolders := h.localTree(top)
	folder := h.drive.lookup(top)
	if folder == nil {
		h.t.Fatalf("remote %s missing", top)
	}
	remoteFiles, remoteFolders := h.drive.tree(folder.ID)
	delete(localFolders, top)
	for p, content := range remoteFiles {
		if localFiles[top+"/"+p] != content {
			h.t.Fatalf("not converged at %s/%s: local %q remote %q", top, p, localFiles[top+"/"+p], content)
		}
	}
	if len(localFiles) != len(remoteFiles) {
		h.t.Fatalf("not converged: %d local files vs %d remote files\nlocal %v\nremote %v", len(localFiles), len(remoteFiles), localFiles, remoteFiles)
	}
	for p := range remoteFolders {
		if !localFolders[top+"/"+p] {
			h.t.Fatalf("remote folder %s/%s missing locally", top, p)
		}
	}
	if len(localFolders) != len(remoteFolders) {
		h.t.Fatalf("not converged: local folders %v remote folders %v", localFolders, remoteFolders)
	}
}
