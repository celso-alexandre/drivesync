package main

import (
	"fmt"
	"math/rand/v2"
	"os"
	"path"
	"slices"
	"strings"
	"testing"
)

func TestRandomizedNeverLosesDataAndConverges(t *testing.T) {
	seeds := 60
	if testing.Short() {
		seeds = 10
	}
	for seed := 1; seed <= seeds; seed++ {
		t.Run(fmt.Sprint(seed), func(t *testing.T) {
			randomizedRun(t, uint64(seed))
		})
	}
}

func randomizedRun(t *testing.T, seed uint64) {
	h := newHarness(t)
	h.sync.allowDelete = true
	rng := rand.New(rand.NewPCG(seed, 0))
	h.drive.add("Work", "root-id", nil, true)
	dirs := []string{"", "x", "x/y", "z"}
	names := []string{"a.txt", "b.txt", "c.bin", "d.md"}
	counter := 0
	nextContent := func() string {
		counter++
		return fmt.Sprintf("seed%d-v%d", seed, counter)
	}
	randomPath := func() string {
		return path.Join(dirs[rng.IntN(len(dirs))], names[rng.IntN(len(names))])
	}
	ensureRemoteDir := func(dir string) string {
		parent := h.drive.lookup("Work").ID
		if dir == "" {
			return parent
		}
		current := "Work"
		for _, part := range strings.Split(dir, "/") {
			current += "/" + part
			if existing := h.drive.lookup(current); existing != nil {
				parent = existing.ID
				continue
			}
			parent = h.drive.add(part, parent, nil, true)
		}
		return parent
	}
	localFiles := func() []string {
		files, _ := h.localTree("Work")
		var list []string
		for p := range files {
			list = append(list, p)
		}
		slices.Sort(list)
		return list
	}
	remoteFiles := func() []string {
		files, _ := h.drive.tree(h.drive.lookup("Work").ID)
		var list []string
		for p := range files {
			list = append(list, p)
		}
		slices.Sort(list)
		return list
	}

	for round := 0; round < 30; round++ {
		for range rng.IntN(5) {
			switch rng.IntN(10) {
			case 0, 1, 2:
				h.write("Work/"+randomPath(), nextContent())
			case 3:
				if files := localFiles(); len(files) > 0 {
					os.Remove(h.full(files[rng.IntN(len(files))]))
				}
			case 4:
				os.RemoveAll(h.full("Work/" + dirs[1+rng.IntN(len(dirs)-1)]))
			case 5, 6, 7:
				p := randomPath()
				if existing := h.drive.lookup("Work/" + p); existing != nil {
					h.drive.edit(existing.ID, []byte(nextContent()))
				} else {
					dir := path.Dir(p)
					if dir == "." {
						dir = ""
					}
					h.drive.add(path.Base(p), ensureRemoteDir(dir), []byte(nextContent()), false)
				}
			case 8:
				if files := remoteFiles(); len(files) > 0 {
					h.drive.trash(h.drive.lookup("Work/" + files[rng.IntN(len(files))]).ID)
				}
			case 9:
				if folder := h.drive.lookup("Work/" + dirs[1+rng.IntN(len(dirs)-1)]); folder != nil {
					h.drive.trash(folder.ID)
				}
			}
		}
		if rng.IntN(4) == 0 {
			h.restart()
		}
		before := h.allContents()
		h.run()
		after := h.allContents()
		for content := range before {
			if !after[content] {
				t.Fatalf("round %d: content %q was lost by sync", round, content)
			}
		}
	}

	for range 3 {
		h.run()
	}
	h.assertConverged("Work")

	revision := h.drive.revision
	snapshot, _ := h.localTree("")
	h.run()
	h.restart()
	h.run()
	again, _ := h.localTree("")
	if h.drive.revision != revision || fmt.Sprint(snapshot) != fmt.Sprint(again) {
		t.Fatal("a converged tree was changed by another pass")
	}
}
