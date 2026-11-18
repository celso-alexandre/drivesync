package main

import (
	"fmt"
	"os"
	"strings"
	"testing"
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
	w.stepStarted("Work/a", "down", 10)
	w.addBytes(10)
	w.stepDone()
	w.stepStarted("Work/b", "up", 20)
	w.addBytes(20)
	w.stepDone()
	w.save()
	p := readStatus(t).Progress
	if p == nil || p.FilesDone != 2 || p.BytesDone != 30 || p.Current != "Work/b" || p.Direction != "up" {
		t.Fatalf("progress wrong: %+v", p)
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
	seen := []Progress{}
	h.sync.drive.client.Transport = observeTransport{base: h.drive.server.Client().Transport, before: func() {
		if p := h.sync.status.status.Progress; p != nil {
			seen = append(seen, *p)
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
