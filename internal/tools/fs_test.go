package tools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func fsToolByName(ts []Tool, name string) Tool {
	for _, t := range ts {
		if t.Name() == name {
			return t
		}
	}
	return nil
}

func TestFSWriteThenRead(t *testing.T) {
	root := t.TempDir()
	ts := NewFSTools(root)
	write := fsToolByName(ts, "fs.write")
	read := fsToolByName(ts, "fs.read")
	if write == nil || read == nil {
		t.Fatal("fs.write / fs.read not registered")
	}
	if _, err := write.Execute(context.Background(),
		json.RawMessage(`{"path":"note.txt","content":"hello"}`)); err != nil {
		t.Fatalf("write failed: %v", err)
	}
	res, err := read.Execute(context.Background(), json.RawMessage(`{"path":"note.txt"}`))
	if err != nil {
		t.Fatalf("read failed: %v", err)
	}
	if !strings.Contains(res.Display, "hello") {
		t.Fatalf("read did not return content: %q", res.Display)
	}
}

func TestFSEdit(t *testing.T) {
	root := t.TempDir()
	ts := NewFSTools(root)
	w := fsToolByName(ts, "fs.write")
	e := fsToolByName(ts, "fs.edit")
	r := fsToolByName(ts, "fs.read")
	_, _ = w.Execute(context.Background(), json.RawMessage(`{"path":"f.txt","content":"foo bar foo"}`))
	if _, err := e.Execute(context.Background(),
		json.RawMessage(`{"path":"f.txt","old":"bar","new":"baz"}`)); err != nil {
		t.Fatalf("edit failed: %v", err)
	}
	res, _ := r.Execute(context.Background(), json.RawMessage(`{"path":"f.txt"}`))
	if !strings.Contains(res.Display, "foo baz foo") {
		t.Fatalf("edit not applied: %q", res.Display)
	}
}

func TestFSReadRejectsEscape(t *testing.T) {
	ts := NewFSTools(t.TempDir())
	r := fsToolByName(ts, "fs.read")
	if _, err := r.Execute(context.Background(), json.RawMessage(`{"path":"../../etc/passwd"}`)); err == nil {
		t.Fatal("read of an escaping path should fail")
	}
}

func TestFSBashDenylist(t *testing.T) {
	ts := NewFSTools(t.TempDir())
	b := fsToolByName(ts, "fs.bash")
	if !b.RequiresConfirmation(json.RawMessage(`{"command":"rm -rf /"}`)) {
		t.Error("rm -rf must require confirmation")
	}
	if !b.RequiresConfirmation(json.RawMessage(`{"command":"sudo reboot"}`)) {
		t.Error("sudo must require confirmation")
	}
	if b.RequiresConfirmation(json.RawMessage(`{"command":"ls -la"}`)) {
		t.Error("ls must not require confirmation")
	}
}

func TestFSBashRuns(t *testing.T) {
	ts := NewFSTools(t.TempDir())
	b := fsToolByName(ts, "fs.bash")
	res, err := b.Execute(context.Background(), json.RawMessage(`{"command":"echo proteus"}`))
	if err != nil {
		t.Fatalf("bash failed: %v", err)
	}
	if !strings.Contains(res.Display, "proteus") {
		t.Fatalf("bash output missing: %q", res.Display)
	}
}

func TestFSBashAllowlistPermits(t *testing.T) {
	ts := NewFSTools(t.TempDir())
	b := fsToolByName(ts, "fs.bash")
	res, err := b.Execute(context.Background(), json.RawMessage(`{"command":"ls"}`))
	if err != nil {
		t.Fatalf("allowlisted command ls failed: %v", err)
	}
	if strings.Contains(res.Display, "command not found") {
		t.Fatalf("allowlisted command ls not found: %q", res.Display)
	}
}

func TestFSBashAllowlistBlocks(t *testing.T) {
	ts := NewFSTools(t.TempDir())
	b := fsToolByName(ts, "fs.bash")
	res, err := b.Execute(context.Background(), json.RawMessage(`{"command":"whoami"}`))
	if err == nil && !strings.Contains(res.Display, "not found") {
		t.Fatalf("non-allowlisted command whoami should not resolve: %q", res.Display)
	}
}
