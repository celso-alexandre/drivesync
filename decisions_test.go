package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func massDeleteSetup(t *testing.T) (*harness, string, []string) {
	h := newHarness(t)
	h.sync.status = newStatusWriter()
	work := h.drive.add("Work", "root-id", nil, true)
	var ids []string
	for i := range 30 {
		ids = append(ids, h.drive.add(fmt.Sprintf("f%d.txt", i), work, []byte(fmt.Sprint("content ", i)), false))
	}
	h.run()
	return h, work, ids
}

func answer(t *testing.T, id, choice string) {
	t.Helper()
	cfg, _ := loadConfig()
	cfg.Decisions = map[string]string{id: choice}
	if err := saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
}

func pendingAttention(t *testing.T) Attention {
	t.Helper()
	status := readStatus(t)
	if len(status.Attention) != 1 || status.Attention[0].Kind != "mass_delete" {
		t.Fatalf("expected one mass delete question, got %+v", status.Attention)
	}
	return status.Attention[0]
}

func TestMassDeleteAsksAndKeepRestoresLocalFiles(t *testing.T) {
	h, _, ids := massDeleteSetup(t)
	for i := range 30 {
		os.Remove(h.full(fmt.Sprintf("Work/f%d.txt", i)))
	}
	h.run()
	question := pendingAttention(t)
	if question.Folder != "Work" || question.Count != 30 || !strings.HasPrefix(question.ID, "mass_delete:Work:") {
		t.Fatalf("unexpected question %+v", question)
	}
	h.run()
	if again := pendingAttention(t); again.ID != question.ID {
		t.Fatal("the question changed identity between passes")
	}
	answer(t, question.ID, decisionKeep)
	h.run()
	for i, id := range ids {
		if h.drive.files[id].trashed {
			t.Fatal("keep still trashed files in Drive")
		}
		if h.read(fmt.Sprintf("Work/f%d.txt", i)) != fmt.Sprint("content ", i) {
			t.Fatal("keep did not restore the local copy")
		}
	}
	if len(readStatus(t).Attention) != 0 {
		t.Fatal("question still open after answering")
	}
	h.run()
	h.assertConverged("Work")
}

func TestMassDeleteDeleteAnswerTrashes(t *testing.T) {
	h, _, ids := massDeleteSetup(t)
	for i := range 30 {
		os.Remove(h.full(fmt.Sprintf("Work/f%d.txt", i)))
	}
	h.run()
	answer(t, pendingAttention(t).ID, decisionDelete)
	h.run()
	for _, id := range ids {
		if !h.drive.files[id].trashed {
			t.Fatal("delete answer did not move files to the Drive trash")
		}
	}
	if len(readStatus(t).Attention) != 0 {
		t.Fatal("question still open after answering")
	}
}

func TestMassRemoteDeleteKeepReuploads(t *testing.T) {
	h, work, ids := massDeleteSetup(t)
	for _, id := range ids {
		h.drive.trash(id)
	}
	h.run()
	answer(t, pendingAttention(t).ID, decisionKeep)
	h.run()
	for i := range 30 {
		name := fmt.Sprintf("f%d.txt", i)
		restored := h.drive.find(work, name)
		if restored == nil || string(restored.content) != fmt.Sprint("content ", i) {
			t.Fatalf("%s was not restored to Drive", name)
		}
		if !h.exists("Work/" + name) {
			t.Fatal("local copy was removed")
		}
	}
	h.run()
	h.assertConverged("Work")
}

func TestOldAnswerDoesNotApplyToANewIncident(t *testing.T) {
	h, _, _ := massDeleteSetup(t)
	for i := range 30 {
		os.Remove(h.full(fmt.Sprintf("Work/f%d.txt", i)))
	}
	h.run()
	first := pendingAttention(t)
	answer(t, first.ID, decisionKeep)
	h.run()
	for i := range 30 {
		os.Remove(h.full(fmt.Sprintf("Work/f%d.txt", i)))
	}
	h.run()
	second := pendingAttention(t)
	if second.ID == first.ID {
		t.Fatal("a new incident reused the old answer's identity")
	}
	for i := range 30 {
		if h.exists(fmt.Sprintf("Work/f%d.txt", i)) {
			t.Fatal("the old keep answer was applied to the new incident")
		}
	}
}

func TestAccountIsReported(t *testing.T) {
	h := newHarness(t)
	h.sync.status = newStatusWriter()
	h.drive.add("Work", "root-id", nil, true)
	h.run()
	if readStatus(t).Account != "tester@example.com" {
		t.Fatalf("account not reported: %q", readStatus(t).Account)
	}
}

func TestProgressCountsAndClears(t *testing.T) {
	newHarness(t)
	w := newStatusWriter()
	w.startProgress(2, 30)
	addA := w.startTransfer("Work/a", "down", 10)
	addB := w.startTransfer("Work/b", "up", 20)
	addA(10)
	addB(5)
	p := w.progressSnapshot()
	if p == nil || len(p.Active) != 2 || p.BytesDone != 15 || p.Active[0].Done != 10 || p.Active[1].Done != 5 {
		t.Fatalf("progress wrong: %+v", p)
	}
	w.endTransfer("Work/a")
	w.stepDone()
	addB(15)
	w.endTransfer("Work/b")
	w.stepDone()
	w.mu.Lock()
	w.save()
	w.mu.Unlock()
	saved := readStatus(t).Progress
	if saved == nil || saved.FilesDone != 2 || saved.BytesDone != 30 || len(saved.Active) != 0 {
		t.Fatalf("saved progress wrong: %+v", saved)
	}
	w.endProgress()
	if readStatus(t).Progress != nil {
		t.Fatal("progress not cleared")
	}
}

func TestPassReportsTotalsBeforeCopying(t *testing.T) {
	h := newHarness(t)
	h.sync.status = newStatusWriter()
	work := h.drive.add("Work", "root-id", nil, true)
	h.drive.add("a.txt", work, []byte("12345"), false)
	h.drive.add("b.txt", work, []byte("123"), false)
	var mu sync.Mutex
	seen := []Progress{}
	h.sync.drive.client.Transport = observeTransport{base: h.drive.server.Client().Transport, before: func() {
		if p := h.sync.status.progressSnapshot(); p != nil {
			mu.Lock()
			seen = append(seen, *p)
			mu.Unlock()
		}
	}}
	h.run()
	if len(seen) == 0 || seen[len(seen)-1].Files != 2 || seen[len(seen)-1].Bytes != 8 {
		t.Fatalf("totals not known while copying: %+v", seen)
	}
	if readStatus(t).Progress != nil {
		t.Fatal("progress left behind after the pass")
	}
}

func TestTransfersRunInParallel(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	for i := range 24 {
		h.drive.add(fmt.Sprintf("f%d.bin", i), work, []byte(fmt.Sprint("data ", i)), false)
	}
	var inFlight, peak atomic.Int32
	h.sync.drive.client.Transport = observeTransport{base: h.drive.server.Client().Transport, before: func() {
		n := inFlight.Add(1)
		for {
			old := peak.Load()
			if n <= old || peak.CompareAndSwap(old, n) {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
		inFlight.Add(-1)
	}}
	h.run()
	if peak.Load() < 2 {
		t.Fatalf("transfers never overlapped (peak %d)", peak.Load())
	}
	if peak.Load() > transferWorkers {
		t.Fatalf("more than %d transfers at once (peak %d)", transferWorkers, peak.Load())
	}
	h.assertConverged("Work")
}

func TestParallelUploadsCreateEachFolderOnce(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	h.run()
	for i := range 20 {
		h.write(fmt.Sprintf("Work/new/deeper/f%d.txt", i), fmt.Sprint("local ", i))
	}
	h.run()
	created := 0
	for _, file := range h.drive.files {
		if file.Name == "new" && file.MimeType == folderMime && !file.trashed && file.Parents[0] == work {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("parallel uploads created %d copies of the new folder", created)
	}
	deeper := h.drive.lookup("Work/new/deeper")
	if deeper == nil {
		t.Fatal("nested folder missing")
	}
	files, _ := h.drive.tree(deeper.ID)
	if len(files) != 20 {
		t.Fatalf("expected 20 uploaded files, got %d", len(files))
	}
	h.run()
	h.assertConverged("Work")
}
