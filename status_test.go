package main

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
)

func readStatus(t *testing.T) Status {
	t.Helper()
	dir, _ := stateDir()
	data, err := os.ReadFile(dir + "/status.json")
	if err != nil {
		t.Fatal(err)
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		t.Fatal(err)
	}
	return status
}

func TestStatusReportsActivity(t *testing.T) {
	h := newHarness(t)
	h.sync.status = newStatusWriter()
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("v1"), false)
	h.run()
	status := readStatus(t)
	if status.State != statusIdle || status.LastSync.IsZero() {
		t.Fatalf("after a pass: state %q, last sync %v", status.State, status.LastSync)
	}
	if len(status.Recent) == 0 || status.Recent[0].Kind != eventDownloaded || status.Recent[0].Path != "Work/a.txt" {
		t.Fatalf("download not reported: %+v", status.Recent)
	}
	h.drive.edit(file, []byte("remote"))
	h.write("Work/a.txt", "local")
	h.run()
	found := false
	for _, event := range readStatus(t).Recent {
		if event.Kind == eventConflict {
			found = true
		}
	}
	if !found {
		t.Fatal("conflict not reported")
	}
}

func TestStatusKeepsOnlyRecentEvents(t *testing.T) {
	newHarness(t)
	w := newStatusWriter()
	for range recentEventLimit + 20 {
		w.event(eventUploaded, "x", "")
	}
	if len(readStatus(t).Recent) != recentEventLimit {
		t.Fatal("recent events are not capped")
	}
}

func TestPausedDoesNothing(t *testing.T) {
	h := newHarness(t)
	h.sync.status = newStatusWriter()
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("v1"), false)
	h.run()
	cfg, _ := loadConfig()
	cfg.Paused = true
	saveConfig(cfg)
	h.drive.edit(file, []byte("v2"))
	h.write("Work/b.txt", "new")
	h.run()
	if h.read("Work/a.txt") != "v1" || h.drive.find(work, "b.txt") != nil {
		t.Fatal("a paused sync still transferred files")
	}
	if readStatus(t).State != statusPaused {
		t.Fatal("status does not say paused")
	}
	cfg.Paused = false
	saveConfig(cfg)
	h.run()
	if h.read("Work/a.txt") != "v2" || h.drive.find(work, "b.txt") == nil {
		t.Fatal("resuming did not catch up")
	}
}

func TestNoFoldersStatus(t *testing.T) {
	h := newHarness(t)
	h.sync.status = newStatusWriter()
	cfg, _ := loadConfig()
	cfg.Folders = nil
	saveConfig(cfg)
	h.run()
	if readStatus(t).State != statusNoFolders {
		t.Fatal("status does not say no folders")
	}
}

func TestSettingsChangeWakesTheLoop(t *testing.T) {
	newHarness(t)
	go func() {
		time.Sleep(1500 * time.Millisecond)
		cfg, _ := loadConfig()
		cfg.SyncRequested = time.Now().UnixNano()
		saveConfig(cfg)
	}()
	start := time.Now()
	if !waitForNextPass(context.Background(), time.Minute) {
		t.Fatal("wait was cancelled")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("a settings change took %v to wake the loop", elapsed)
	}
}

func TestWaitStopsOnCancel(t *testing.T) {
	newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(500 * time.Millisecond)
		cancel()
	}()
	if waitForNextPass(ctx, time.Minute) {
		t.Fatal("wait did not stop on cancel")
	}
}
