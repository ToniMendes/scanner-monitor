package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDuplicateContentPreservesSource(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	path := filepath.Join(src, "new.pdf")
	writePDF(t, path, "identical")
	writePDF(t, filepath.Join(dst, "old.pdf"), "identical")
	for i := 0; i < 2; i++ {
		if err := NewFileSystem().MovePDF(path, dst); !errors.Is(err, ErrDuplicate) {
			t.Fatalf("expected duplicate: %v", err)
		}
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != "identical" {
		t.Fatalf("source lost: %q %v", data, err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil || len(entries) != 1 {
		t.Fatalf("%v %v", entries, err)
	}
}
func TestMoveContextCancelled(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	path := filepath.Join(src, "test.pdf")
	writePDF(t, path, "content")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewFileSystem().MovePDFContext(ctx, path, dst); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
func TestPartialPDFWaitsUntilComplete(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scan.pdf")
	writePDF(t, path, "%PDF-1.7\nunfinished")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	files := make(chan *os.File)
	done := make(chan error, 1)
	fs := &FileSystem{pollInterval: 10 * time.Millisecond}
	go func() { done <- fs.GetPdf(ctx, files, root) }()
	select {
	case file := <-files:
		file.Close()
		t.Fatal("incomplete PDF delivered")
	case <-time.After(60 * time.Millisecond):
	}
	writePDF(t, path, "%PDF-1.7\ncomplete\n%%EOF")
	select {
	case file := <-files:
		file.Close()
	case <-time.After(time.Second):
		t.Fatal("complete PDF not delivered")
	}
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not stop")
	}
}
func TestCompletePDFMarkers(t *testing.T) {
	for _, tc := range []struct {
		content string
		want    bool
	}{{"", false}, {"garbage", false}, {"%PDF-1.7\nbody", false}, {"wrong\n%%EOF", false}, {"%PDF-1.7\nbody\n%%EOF\r\n", true}} {
		path := filepath.Join(t.TempDir(), "scan.pdf")
		writePDF(t, path, tc.content)
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		got := completePDF(file, int64(len(tc.content)))
		file.Close()
		if got != tc.want {
			t.Fatalf("%q: %v", tc.content, got)
		}
	}
}
func TestRemoveChangedSourcePreservesFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.pdf")
	writePDF(t, path, "first")
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	writePDF(t, path, "new larger data")
	if err := removeUnchanged(path, before); err == nil {
		t.Fatal("changed file removed")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
}
func TestPublicationFailureCleansTemporary(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	path := filepath.Join(src, "scan.pdf")
	writePDF(t, path, "complete content")
	sentinel := errors.New("publication failed")
	fs := &FileSystem{publish: func(temp, target string) error {
		data, err := os.ReadFile(temp)
		if err != nil || string(data) != "complete content" {
			t.Fatalf("incomplete temp: %q %v", data, err)
		}
		if _, err := os.Stat(target); !os.IsNotExist(err) {
			t.Fatal("target published prematurely")
		}
		return sentinel
	}}
	if err := fs.MovePDF(path, dst); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary not cleaned: %v %v", entries, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("source lost")
	}
}
func TestSourceRemovalFailureDoesNotPublishTwice(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	path := filepath.Join(src, "scan.pdf")
	writePDF(t, path, "complete content")
	sentinel := errors.New("source locked")
	fs := &FileSystem{removeSource: func(string, os.FileInfo) error { return sentinel }}
	if err := fs.MovePDF(path, dst); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	if err := fs.MovePDF(path, dst); !errors.Is(err, ErrDuplicate) {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dst)
	if err != nil || len(entries) != 1 {
		t.Fatalf("duplicate published: %v %v", entries, err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal("source lost")
	}
}
func TestContextReaderCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reader := &contextReader{ctx: ctx}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestMoveCancellationWhileWaiting(t *testing.T) {
	moveGate <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	defer func() { <-moveGate }()
	if err := NewFileSystem().MovePDFContext(ctx, "source", "dest"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
