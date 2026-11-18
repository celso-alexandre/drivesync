package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestNoThirdPartyDependencies(t *testing.T) {
	data, err := os.ReadFile("go.mod")
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && (fields[0] == "require" || fields[0] == "replace" || fields[0] == "tool" || fields[0] == "toolchain") {
			t.Fatalf("go.mod must stay dependency-free, found: %s", line)
		}
	}
	if _, err := os.Stat("go.sum"); err == nil {
		t.Fatal("go.sum exists, so a dependency was added")
	}
	files, _ := filepath.Glob("*.go")
	for _, file := range files {
		parsed, err := parser.ParseFile(token.NewFileSet(), file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			first, _, _ := strings.Cut(path, "/")
			if strings.Contains(first, ".") {
				t.Errorf("%s imports %s; only the standard library is allowed", file, path)
			}
			if path == "C" || path == "plugin" {
				t.Errorf("%s imports %s, which would weaken the sandbox", file, path)
			}
		}
	}
}
