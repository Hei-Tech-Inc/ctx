package tui

import (
	"io"
	"os"
	"testing"
)

// TestFormInputEnded: once its form has ended, a formInput reads nothing, so
// a stale bubbletea reader cannot take the next form's keys; they stay for
// the next formInput.
func TestFormInputEnded(t *testing.T) {
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = pr.Close(); _ = pw.Close() }()
	old, cur := &formInput{File: pr}, &formInput{File: pr}
	old.ended.Store(true)
	if _, err := pw.Write([]byte("\r")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 8)
	if n, err := old.Read(buf); n != 0 || err != io.EOF {
		t.Fatalf("ended Read = %d, %v; want 0, EOF", n, err)
	}
	if n, err := cur.Read(buf); err != nil || string(buf[:n]) != "\r" {
		t.Fatalf("live Read = %q, %v; want \"\\r\"", buf[:n], err)
	}
	if err := cur.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := pw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if n, err := pr.Read(buf); err != nil || string(buf[:n]) != "x" {
		t.Fatalf("after formInput.Close the file reads %q, %v", buf[:n], err)
	}
}
