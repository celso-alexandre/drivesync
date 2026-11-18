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

type Status struct {
	State    string    `json:"state"`
	Message  string    `json:"message,omitempty"`
	LastSync time.Time `json:"last_sync,omitzero"`
	Updated  time.Time `json:"updated"`
	Recent   []Event   `json:"recent"`
}

type statusWriter struct {
	path   string
	status Status
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
	data, err := json.Marshal(w.status)
	if err != nil {
		return
	}
	if err := writeFileAtomic(w.path, data, 0o600); err != nil {
		log.Printf("writing status: %v", err)
	}
}
