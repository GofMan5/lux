package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFilesystemWorkspaceValidatesAndReplacesExistingFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "config.lux")
	if err := os.WriteFile(path, []byte("name = old\nworkers = 1\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	source := fsWorkspaceSource{root: root}
	if err := source.Write(context.Background(), "config.lux", "name = new\nworkers = 4\n"); err != nil {
		t.Fatalf("write: %v", err)
	}
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(contents) != "name = new\nworkers = 4\n" {
		t.Fatalf("contents=%q", contents)
	}
	if _, err := os.Stat(path + ".lux-backup"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("backup leaked: %v", err)
	}
	if err := source.Write(context.Background(), "../escape", "nope"); err == nil {
		t.Fatal("path traversal must be rejected")
	}
}

func TestValidateWorkspaceConfig(t *testing.T) {
	for _, test := range []struct {
		name, text, want string
	}{
		{"valid", "name = demo\nworkers = 2\n", ""},
		{"missing name", "workers = 2\n", "name is required"},
		{"bad workers", "name = demo\nworkers = 0\n", "workers must be a positive integer"},
		{"syntax", "name demo\nworkers = 2\n", "line must use key = value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := validateWorkspaceFile("config.lux", test.text); (err == nil) != (test.want == "") || err != nil && err.Error() != test.want {
				t.Fatalf("error=%v want=%q", err, test.want)
			}
		})
	}
}
