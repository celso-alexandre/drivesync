package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	statusStarting  = "starting"
	statusSyncing   = "syncing"
	statusIdle      = "idle"
	statusPaused    = "paused"
	statusError     = "error"
	statusSignedOut = "signed_out"
	statusNoFolders = "no_folders"

	eventDownloaded     = "downloaded"
	eventUploaded       = "uploaded"
	eventConflict       = "conflict"
	eventDeletedInDrive = "deleted_in_drive"
	eventDeletedLocally = "deleted_locally"
	eventFolderCreated  = "folder_created"
	eventRenamed        = "renamed"
	eventWarning        = "warning"
	eventError          = "error"

	recentEventLimit = 50
)

type Event struct {
	Time   time.Time `json:"time"`
	Kind   string    `json:"kind"`
	Path   string    `json:"path,omitempty"`
	Detail string    `json:"detail,omitempty"`
}

type Attention struct {
	ID     string `json:"id"`
	Kind   string `json:"kind"`
	Folder string `json:"folder,omitempty"`
	Count  int    `json:"count,omitempty"`
	Total  int    `json:"total,omitempty"`
}

type Transfer struct {
	Path      string `json:"path"`
	Direction string `json:"direction"`
	Bytes     int64  `json:"bytes"`
	Done      int64  `json:"done"`
}

type Progress struct {
	Files     int        `json:"files"`
	FilesDone int        `json:"files_done"`
	Bytes     int64      `json:"bytes"`
	BytesDone int64      `json:"bytes_done"`
	Active    []Transfer `json:"active"`
}

type Status struct {
	State     string      `json:"state"`
	Message   string      `json:"message,omitempty"`
	Account   string      `json:"account,omitempty"`
	LastSync  time.Time   `json:"last_sync,omitzero"`
	Updated   time.Time   `json:"updated"`
	Progress  *Progress   `json:"progress,omitempty"`
	Attention []Attention `json:"attention"`
	Recent    []Event     `json:"recent"`
}

type statusWriter struct {
	mu        sync.Mutex
	path      string
	status    Status
	attention []Attention
	lastSave  time.Time
}

const progressSaveInterval = 500 * time.Millisecond

func (w *statusWriter) setAccount(account string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status.Account == account {
		return
	}
	w.status.Account = account
	w.save()
}

func (w *statusWriter) beginAttention() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.attention = []Attention{}
}

func (w *statusWriter) needAttention(item Attention) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.attention = append(w.attention, item)
}

func (w *statusWriter) endAttention() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status.Attention = w.attention
	w.save()
}

func (w *statusWriter) startProgress(files int, bytes int64) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if files == 0 {
		w.status.Progress = nil
		return
	}
	w.status.Progress = &Progress{Files: files, Bytes: bytes, Active: []Transfer{}}
	w.status.State = statusSyncing
	w.status.Message = ""
	w.save()
}

func (w *statusWriter) startTransfer(path, direction string, size int64) func(int64) {
	if w == nil {
		return func(int64) {}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status.Progress != nil {
		w.status.Progress.Active = append(w.status.Progress.Active, Transfer{Path: path, Direction: direction, Bytes: size})
		w.saveThrottled()
	}
	return func(n int64) {
		w.mu.Lock()
		defer w.mu.Unlock()
		p := w.status.Progress
		if p == nil {
			return
		}
		p.BytesDone += n
		for i := range p.Active {
			if p.Active[i].Path == path {
				p.Active[i].Done += n
				break
			}
		}
		w.saveThrottled()
	}
}

func (w *statusWriter) endTransfer(path string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	p := w.status.Progress
	if p == nil {
		return
	}
	for i := range p.Active {
		if p.Active[i].Path == path {
			p.Active = append(p.Active[:i], p.Active[i+1:]...)
			break
		}
	}
	w.saveThrottled()
}

func (w *statusWriter) stepDone() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status.Progress == nil {
		return
	}
	w.status.Progress.FilesDone++
	w.saveThrottled()
}

func (w *statusWriter) endProgress() {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status.Progress == nil {
		return
	}
	w.status.Progress = nil
	w.save()
}

func (w *statusWriter) progressSnapshot() *Progress {
	if w == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.status.Progress == nil {
		return nil
	}
	copied := *w.status.Progress
	copied.Active = append([]Transfer(nil), w.status.Progress.Active...)
	return &copied
}

func (w *statusWriter) saveThrottled() {
	if time.Since(w.lastSave) >= progressSaveInterval {
		w.save()
	}
}

func newStatusWriter() *statusWriter {
	dir, err := stateDir()
	if err != nil {
		return &statusWriter{}
	}
	w := &statusWriter{path: filepath.Join(dir, "status.json")}
	if data, err := os.ReadFile(w.path); err == nil {
		_ = json.Unmarshal(data, &w.status)
	}
	if w.status.Recent == nil {
		w.status.Recent = []Event{}
	}
	if w.status.Attention == nil {
		w.status.Attention = []Attention{}
	}
	w.status.Progress = nil
	return w
}

func (w *statusWriter) set(state, message string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.setLocked(state, message)
}

func (w *statusWriter) setLocked(state, message string) {
	if state == statusIdle {
		w.status.LastSync = time.Now()
	}
	if w.status.State == state && w.status.Message == message && state != statusIdle {
		return
	}
	w.status.State = state
	w.status.Message = message
	w.save()
}

func (w *statusWriter) leaveWaitingState(message string) {
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	switch w.status.State {
	case "", statusStarting, statusNoFolders, statusPaused, statusSignedOut:
		w.setLocked(statusSyncing, message)
	}
}

func (w *statusWriter) event(kind, path, detail string) {
	if detail != "" {
		log.Printf("%s: %s", path, detail)
	} else {
		log.Printf("%s: %s", path, kind)
	}
	if w == nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	w.status.Recent = append([]Event{{Time: time.Now(), Kind: kind, Path: path, Detail: detail}}, w.status.Recent...)
	if len(w.status.Recent) > recentEventLimit {
		w.status.Recent = w.status.Recent[:recentEventLimit]
	}
	if kind != eventWarning && kind != eventError && w.status.State != statusSyncing {
		w.status.State = statusSyncing
		w.status.Message = ""
	}
	w.save()
}

func (w *statusWriter) save() {
	if w == nil || w.path == "" {
		return
	}
	w.status.Updated = time.Now()
	w.lastSave = w.status.Updated
	data, err := json.Marshal(w.status)
	if err != nil {
		return
	}
	if err := writeFileAtomic(w.path, data, 0o600); err != nil {
		log.Printf("writing status: %v", err)
	}
}
