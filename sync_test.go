package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func twoWayScenario(t *testing.T, h *harness) {
	work := h.drive.add("Work", "root-id", nil, true)
	h.drive.add("Other", "root-id", nil, true)
	docs := h.drive.add("docs", work, nil, true)
	report := h.drive.add("report.txt", docs, []byte("v1"), false)
	h.drive.add("empty", work, nil, true)

	h.run()
	if got := h.read("Work/docs/report.txt"); got != "v1" {
		t.Fatalf("initial download: got %q", got)
	}
	if !h.exists("Work/empty") || h.exists("Other") {
		t.Fatal("selective sync created the wrong folders")
	}
	h.assertConverged("Work")

	h.write("Work/new/notes.md", "hello")
	h.run()
	newFolder := h.drive.find(work, "new")
	if newFolder == nil || h.drive.find(newFolder.ID, "notes.md") == nil {
		t.Fatal("local new file was not uploaded with its folder")
	}

	h.write("Work/docs/report.txt", "v2-local")
	h.run()
	if got := string(h.drive.files[report].content); got != "v2-local" {
		t.Fatalf("local edit not uploaded: %q", got)
	}

	h.drive.edit(report, []byte("v3-remote"))
	h.run()
	if got := h.read("Work/docs/report.txt"); got != "v3-remote" {
		t.Fatalf("remote edit not downloaded: %q", got)
	}

	h.drive.edit(report, []byte("both-remote"))
	h.write("Work/docs/report.txt", "both-local")
	h.run()
	if got := h.read("Work/docs/report.txt"); got != "both-remote" {
		t.Fatalf("conflict: expected remote version in place, got %q", got)
	}
	matches, _ := filepath.Glob(h.full("Work/docs/report (conflict *).txt"))
	if len(matches) != 1 {
		t.Fatalf("conflict copy missing: %v", matches)
	}
	if data, _ := os.ReadFile(matches[0]); string(data) != "both-local" {
		t.Fatalf("conflict copy holds %q", data)
	}
	h.run()
	if h.drive.find(docs, filepath.Base(matches[0])) == nil {
		t.Fatal("conflict copy was not uploaded")
	}

	os.RemoveAll(h.full("Work/new"))
	h.run()
	if h.drive.find(work, "new") != nil {
		t.Fatal("locally deleted folder still in Drive")
	}
	if !h.drive.files[newFolder.ID].trashed {
		t.Fatal("locally deleted folder was not moved to the Drive trash")
	}

	h.drive.trash(report)
	h.run()
	if h.exists("Work/docs/report.txt") {
		t.Fatal("remotely deleted file still present locally")
	}
	trashed, _ := filepath.Glob(h.full(".drivesync-trash/*/Work/docs/report.txt"))
	if len(trashed) != 1 {
		t.Fatal("remotely deleted file was not kept in the local trash")
	}
	h.run()
	h.assertConverged("Work")
}

func TestTwoWaySync(t *testing.T) {
	twoWayScenario(t, newHarness(t))
}

func TestFirstSyncWithExistingLocalFiles(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	h.drive.add("same.txt", work, []byte("identical"), false)
	differ := h.drive.add("differ.txt", work, []byte("remote side"), false)
	h.write("Work/same.txt", "identical")
	h.write("Work/differ.txt", "local side")
	h.write("Work/only-local.txt", "mine")
	requestsBefore := h.drive.requests
	h.run()
	if h.read("Work/same.txt") != "identical" || h.read("Work/differ.txt") != "remote side" {
		t.Fatal("wrong content after first sync")
	}
	matches, _ := filepath.Glob(h.full("Work/differ (conflict *).txt"))
	if len(matches) != 1 {
		t.Fatal("differing local file was not kept as a conflict copy")
	}
	if h.drive.find(work, "only-local.txt") == nil {
		t.Fatal("local-only file was not uploaded")
	}
	if string(h.drive.files[differ].content) != "remote side" {
		t.Fatal("remote file was overwritten on first sync")
	}
	if h.drive.requests-requestsBefore > 12 {
		t.Fatalf("identical file caused extra transfers: %d requests", h.drive.requests-requestsBefore)
	}
	h.run()
	h.assertConverged("Work")
}

func TestRestartKeepsStateAndDoesNothing(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	h.drive.add("a.txt", work, []byte("a"), false)
	h.write("Work/b.txt", "b")
	h.run()
	h.run()
	revision := h.drive.revision
	before, _ := h.localTree("Work")
	h.restart()
	h.run()
	h.run()
	after, _ := h.localTree("Work")
	if h.drive.revision != revision {
		t.Fatal("restart caused remote changes")
	}
	if fmt.Sprint(before) != fmt.Sprint(after) {
		t.Fatal("restart caused local changes")
	}
}

func TestMetadataOnlyLocalChangeIsNotUploaded(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("same"), false)
	h.run()
	if err := os.Chmod(h.full("Work/a.txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	revision := h.drive.revision
	h.run()
	h.run()
	if h.drive.revision != revision || len(h.drive.files[file].revisions) != 0 {
		t.Fatal("chmod caused an upload")
	}
}

func TestSameSizeSameMtimeEditIsDetected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows exposes no inode change time; edits that restore size and mtime are not detected there")
	}
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("aaaa"), false)
	h.run()
	info, _ := os.Stat(h.full("Work/a.txt"))
	os.WriteFile(h.full("Work/a.txt"), []byte("bbbb"), 0o644)
	os.Chtimes(h.full("Work/a.txt"), info.ModTime(), info.ModTime())
	h.run()
	if string(h.drive.files[file].content) != "bbbb" {
		t.Fatal("edit that kept size and mtime was missed")
	}
}

func TestRemoteRenameAndMove(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	sub := h.drive.add("sub", work, nil, true)
	file := h.drive.add("a.txt", work, []byte("content"), false)
	h.run()
	h.drive.move(file, sub, "renamed.txt")
	h.run()
	if h.exists("Work/a.txt") || h.read("Work/sub/renamed.txt") != "content" {
		t.Fatal("remote move was not mirrored")
	}
	h.run()
	h.assertConverged("Work")
	if !h.drive.live(file) {
		t.Fatal("remote move deleted the file in Drive")
	}
}

func TestLocalRenameKeepsData(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("content"), false)
	h.run()
	os.Rename(h.full("Work/a.txt"), h.full("Work/b.txt"))
	h.run()
	h.run()
	h.assertConverged("Work")
	if h.drive.live(file) {
		t.Fatal("old name still live in Drive")
	}
	if string(h.drive.lookup("Work/b.txt").content) != "content" {
		t.Fatal("renamed file missing in Drive")
	}
}

func TestMissingLocalRootDeletesNothing(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("a"), false)
	h.run()
	os.RemoveAll(h.full("Work"))
	h.run()
	h.run()
	if h.drive.files[file].trashed {
		t.Fatal("missing local root caused remote deletion")
	}
}

func TestMissingRemoteRootDeletesNothing(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	h.drive.add("a.txt", work, []byte("a"), false)
	h.run()
	h.drive.trash(work)
	h.run()
	if h.read("Work/a.txt") != "a" {
		t.Fatal("trashing the remote root deleted local files")
	}
}

func TestMassDeleteGuard(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	var ids []string
	for i := range 30 {
		ids = append(ids, h.drive.add(fmt.Sprintf("f%d.txt", i), work, []byte(fmt.Sprint(i)), false))
	}
	h.run()
	for i := range 30 {
		os.Remove(h.full(fmt.Sprintf("Work/f%d.txt", i)))
	}
	h.run()
	for _, id := range ids {
		if h.drive.files[id].trashed {
			t.Fatal("mass delete guard did not stop remote deletion")
		}
	}
	h.sync.allowDelete = true
	h.run()
	for _, id := range ids {
		if !h.drive.files[id].trashed {
			t.Fatal("--allow-delete did not delete")
		}
	}
}

func TestMassRemoteDeleteGuard(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	var ids []string
	for i := range 30 {
		ids = append(ids, h.drive.add(fmt.Sprintf("f%d.txt", i), work, []byte(fmt.Sprint(i)), false))
	}
	h.run()
	for _, id := range ids {
		h.drive.trash(id)
	}
	h.run()
	for i := range 30 {
		if !h.exists(fmt.Sprintf("Work/f%d.txt", i)) {
			t.Fatal("mass delete guard did not stop local deletion")
		}
	}
}

func TestGoogleDocsAreSkippedAndKeepFolderAlive(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	sheets := h.drive.add("sheets", work, nil, true)
	h.drive.addGoogleDoc("budget", sheets)
	h.run()
	entries, _ := os.ReadDir(h.full("Work/sheets"))
	if len(entries) != 0 {
		t.Fatal("Google Doc was written locally")
	}
	os.Remove(h.full("Work/sheets"))
	h.run()
	if h.drive.files[sheets].trashed {
		t.Fatal("folder holding a Google Doc was trashed")
	}
	if !h.exists("Work/sheets") {
		t.Fatal("folder holding a Google Doc was not restored locally")
	}
}

func TestDuplicateRemoteNamesAreNotClobbered(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	first := h.drive.add("same.txt", work, []byte("one"), false)
	second := h.drive.add("same.txt", work, []byte("two"), false)
	h.run()
	h.run()
	if !h.drive.live(first) || !h.drive.live(second) {
		t.Fatal("duplicate names caused a remote deletion")
	}
	if string(h.drive.files[first].content) != "one" || string(h.drive.files[second].content) != "two" {
		t.Fatal("duplicate names caused a remote overwrite")
	}
}

func TestUnusualNames(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	names := []string{"ação ñ 日本.txt", "with space", ".hidden", "empty.bin", "semi;colon&amp", "a'quote\"s"}
	for _, name := range names {
		content := []byte("content of " + name)
		if name == "empty.bin" {
			content = []byte{}
		}
		h.drive.add(name, work, content, false)
	}
	h.drive.add("bad/name", work, []byte("x"), false)
	h.drive.add(".drivesync-evil", work, []byte("x"), false)
	h.run()
	for _, name := range names {
		if !h.exists("Work/" + name) {
			t.Fatalf("%q not downloaded", name)
		}
	}
	if h.exists("Work/.drivesync-evil") || h.exists("Work/bad") {
		t.Fatal("unsafe remote name was written locally")
	}
	h.write("Work/local ação.txt", "")
	h.run()
	if h.drive.find(work, "local ação.txt") == nil {
		t.Fatal("unicode or empty local file not uploaded")
	}
}

func TestRemoteNamesCannotEscapeTheFolder(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	for _, name := range []string{"..", ".", "../escape", "/abs"} {
		h.drive.add(name, work, []byte("x"), false)
	}
	h.run()
	entries, _ := os.ReadDir(h.root)
	for _, entry := range entries {
		if entry.Name() != "Work" {
			t.Fatalf("remote name escaped into %s", entry.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(h.home, "escape")); err == nil {
		t.Fatal("remote name escaped the sync root")
	}
}

func TestUnselectKeepsLocalFilesAndStops(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("a"), false)
	h.run()
	cfg, _ := loadConfig()
	cfg.Folders = nil
	saveConfig(cfg)
	h.run()
	os.Remove(h.full("Work/a.txt"))
	h.drive.edit(file, []byte("changed"))
	h.run()
	if h.drive.files[file].trashed {
		t.Fatal("unselected folder still synced deletions")
	}
	if h.exists("Work/a.txt") {
		t.Fatal("unselected folder still downloads")
	}
	if len(h.sync.state.Entries) != 0 {
		t.Fatal("state kept entries of an unselected folder")
	}
}

func TestReselectAfterUnselectDoesNotDelete(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("a"), false)
	h.run()
	cfg, _ := loadConfig()
	cfg.Folders = nil
	saveConfig(cfg)
	h.run()
	os.Remove(h.full("Work/a.txt"))
	cfg.Folders = []string{"Work"}
	saveConfig(cfg)
	h.run()
	if h.drive.files[file].trashed || h.read("Work/a.txt") != "a" {
		t.Fatal("re-selecting a folder deleted remote files")
	}
}

func TestTransientErrorsRetryWithoutLoss(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	h.drive.add("a.txt", work, []byte("a"), false)
	h.drive.failRequests("GET /drive/v3/files/id", 1)
	h.write("Work/b.txt", "b")
	h.drive.failRequests("PUT /session/", 1)
	h.run()
	h.run()
	h.run()
	h.assertConverged("Work")
}

func TestListingFailureChangesNothing(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	h.drive.add("a.txt", work, []byte("a"), false)
	h.run()
	h.drive.edit(h.drive.lookup("Work/a.txt").ID, []byte("b"))
	h.drive.failRequests("GET /drive/v3/files", 1)
	if err := h.sync.cycle(context.Background()); err == nil {
		t.Fatal("expected listing error")
	}
	if h.read("Work/a.txt") != "a" {
		t.Fatal("failed listing still changed files")
	}
	h.run()
	if h.read("Work/a.txt") != "b" {
		t.Fatal("did not recover after listing failure")
	}
}

func TestLocalEditDuringPassIsNeverOverwritten(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("v1"), false)
	h.run()
	h.drive.edit(file, []byte("remote-v2"))
	h.sync.local = map[string]localItem{}
	info, _ := os.Stat(h.full("Work/a.txt"))
	h.sync.local["Work/a.txt"] = toLocalItem(info)
	h.write("Work/a.txt", "typed during sync")
	err := h.sync.download(context.Background(), "Work/a.txt", &h.drive.files[file].File)
	if err == nil || !strings.Contains(err.Error(), "changed locally") {
		t.Fatalf("download replaced a file edited mid-pass: %v", err)
	}
	if h.read("Work/a.txt") != "typed during sync" {
		t.Fatal("local edit lost")
	}
	h.run()
	h.run()
	matches, _ := filepath.Glob(h.full("Work/a (conflict *).txt"))
	if len(matches) != 1 || h.read("Work/a.txt") != "remote-v2" {
		t.Fatal("mid-pass edit did not end as a conflict copy")
	}
}

func TestNoTempFilesLeftBehind(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	h.drive.add("a.txt", work, []byte("a"), false)
	h.drive.failRequests("GET /drive/v3/files/id", 1)
	h.run()
	h.run()
	found := false
	filepath.WalkDir(h.full("Work"), func(p string, _ os.DirEntry, _ error) error {
		if strings.HasPrefix(filepath.Base(p), tempPrefix) {
			found = true
		}
		return nil
	})
	if found {
		t.Fatal("temporary download file left behind")
	}
}

func TestDownloadedFileKeepsRemoteTime(t *testing.T) {
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("a.txt", work, []byte("a"), false)
	h.drive.files[file].ModifiedTime = time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	h.run()
	info, _ := os.Stat(h.full("Work/a.txt"))
	if !info.ModTime().Equal(h.drive.files[file].ModifiedTime) {
		t.Fatalf("mtime %v, want %v", info.ModTime(), h.drive.files[file].ModifiedTime)
	}
}

func TestRootChangeStopsTheLoop(t *testing.T) {
	h := newHarness(t)
	h.drive.add("Work", "root-id", nil, true)
	cfg, _ := loadConfig()
	cfg.Root = filepath.Join(h.home, "elsewhere")
	saveConfig(cfg)
	if err := h.sync.cycle(context.Background()); !errors.Is(err, errRootChanged) {
		t.Fatalf("expected errRootChanged, got %v", err)
	}
}

func TestStateFileIsPrivate(t *testing.T) {
	h := newHarness(t)
	h.drive.add("Work", "root-id", nil, true)
	h.run()
	info, err := os.Stat(h.sync.statePath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("state file mode %v", info.Mode().Perm())
	}
}

func withCaseInsensitiveNames(t *testing.T) {
	old := caseInsensitiveNames
	caseInsensitiveNames = true
	t.Cleanup(func() { caseInsensitiveNames = old })
}

func TestCaseOnlyRemoteRenameOnCaseInsensitiveSystems(t *testing.T) {
	withCaseInsensitiveNames(t)
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	file := h.drive.add("Report.txt", work, []byte("content"), false)
	h.run()
	h.drive.move(file, work, "report.txt")
	h.run()
	h.run()
	if !h.drive.live(file) {
		t.Fatal("case-only rename deleted the file in Drive")
	}
	if h.read("Work/report.txt") != "content" || h.exists("Work/Report.txt") {
		t.Fatal("case-only rename was not applied locally")
	}
	files, _ := h.drive.tree(work)
	if len(files) != 1 {
		t.Fatalf("case-only rename duplicated files in Drive: %v", files)
	}
}

func TestCaseDuplicatesOnCaseInsensitiveSystems(t *testing.T) {
	withCaseInsensitiveNames(t)
	h := newHarness(t)
	work := h.drive.add("Work", "root-id", nil, true)
	upper := h.drive.add("A.txt", work, []byte("upper"), false)
	lower := h.drive.add("a.txt", work, []byte("lower"), false)
	h.run()
	h.run()
	if !h.drive.live(upper) || !h.drive.live(lower) {
		t.Fatal("case duplicates caused a deletion in Drive")
	}
	if string(h.drive.files[upper].content) != "upper" || string(h.drive.files[lower].content) != "lower" {
		t.Fatal("case duplicates caused an overwrite in Drive")
	}
}

func TestWindowsNamesAreFiltered(t *testing.T) {
	old := targetOS
	targetOS = "windows"
	t.Cleanup(func() { targetOS = old })
	for _, name := range []string{"CON", "con.txt", "a:b", `a\b`, "trailing.", "trailing ", "a?b", "LPT1.log", "tab\tname"} {
		if validName(name) {
			t.Errorf("%q accepted on Windows", name)
		}
	}
	for _, name := range []string{"report.txt", "ação.txt", "CONSOLE.txt", ".hidden", "a b"} {
		if !validName(name) {
			t.Errorf("%q rejected on Windows", name)
		}
	}
}
