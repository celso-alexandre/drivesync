package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
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

type Progress struct {
	Files        int    `json:"files"`
	FilesDone    int    `json:"files_done"`
	Bytes        int64  `json:"bytes"`
	BytesDone    int64  `json:"bytes_done"`
	Current      string `json:"current,omitempty"`
	Direction    string `json:"direction,omitempty"`
	CurrentBytes int64  `json:"current_bytes,omitempty"`
	CurrentDone  int64  `json:"current_done,omitempty"`
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
	path      string
	status    Status
	attention []Attention
	lastSave  time.Time
}

const progressSaveInterval = 500 * time.Millisecond

func (w *statusWriter) setAccount(account string) {
	if w == nil || w.status.Account == account {
		return
	}
	w.status.Account = account
	w.save()
}

func (w *statusWriter) beginAttention() {
	if w == nil {
		return
	}
	w.attention = []Attention{}
}

func (w *statusWriter) needAttention(item Attention) {
	if w == nil {
		return
	}
	w.attention = append(w.attention, item)
}

func (w *statusWriter) endAttention() {
	if w == nil {
		return
	}
	w.status.Attention = w.attention
	w.save()
}

func (w *statusWriter) startProgress(files int, bytes int64) {
	if w == nil {
		return
	}
	if files == 0 {
		w.status.Progress = nil
		return
	}
	w.status.Progress = &Progress{Files: files, Bytes: bytes}
	w.status.State = statusSyncing
	w.status.Message = ""
	w.save()
}

func (w *statusWriter) stepStarted(path, direction string, size int64) {
	if w == nil || w.status.Progress == nil {
		return
	}
	p := w.status.Progress
	p.Current, p.Direction, p.CurrentBytes, p.CurrentDone = path, direction, size, 0
	w.saveThrottled()
}

func (w *statusWriter) addBytes(n int64) {
	if w == nil || w.status.Progress == nil {
		return
	}
	w.status.Progress.CurrentDone += n
	w.status.Progress.BytesDone += n
	w.saveThrottled()
}

func (w *statusWriter) stepDone() {
	if w == nil || w.status.Progress == nil {
		return
	}
	w.status.Progress.FilesDone++
	w.saveThrottled()
}

func (w *statusWriter) endProgress() {
	if w == nil || w.status.Progress == nil {
		return
	}
	w.status.Progress = nil
	w.save()
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
	switch w.status.State {
	case "", statusStarting, statusNoFolders, statusPaused, statusSignedOut:
		w.set(statusSyncing, message)
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
