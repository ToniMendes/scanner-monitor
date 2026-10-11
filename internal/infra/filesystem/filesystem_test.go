package filesystem

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func writePDF(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestCreateFolder(t *testing.T) {
	for _, category := range []string{"atestado", "outro"} {
		t.Run(category, func(t *testing.T) {
			base := t.TempDir()
			for i := 0; i < 2; i++ {
				path, err := NewFileSystem().CreateFolder(category, base)
				if err != nil || path != filepath.Join(base, category) {
					t.Fatalf("%s %v", path, err)
				}
			}
		})
	}
	for _, category := range []string{"", ".", "..", "../escape", "a/b", "a\\b", "C:foo"} {
		t.Run("invalid_"+category, func(t *testing.T) {
			if _, err := NewFileSystem().CreateFolder(category, t.TempDir()); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	t.Run("missing base", func(t *testing.T) {
		if _, err := NewFileSystem().CreateFolder("outro", filepath.Join(t.TempDir(), "missing")); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("empty base", func(t *testing.T) {
		if _, err := NewFileSystem().CreateFolder("outro", ""); err == nil {
			t.Fatal("expected error")
		}
	})
	t.Run("file collision", func(t *testing.T) {
		base := t.TempDir()
		writePDF(t, filepath.Join(base, "outro"), "x")
		if _, err := NewFileSystem().CreateFolder("outro", base); err == nil {
			t.Fatal("expected error")
		}
	})
}
func TestMovePDFDuplicates(t *testing.T) {
	src, dst := t.TempDir(), t.TempDir()
	writePDF(t, filepath.Join(dst, "scan.pdf"), "original")
	writePDF(t, filepath.Join(dst, "scan (1).pdf"), "previous")
	input := filepath.Join(src, "scan.pdf")
	writePDF(t, input, "new")
	if err := NewFileSystem().MovePDF(input, dst); err != nil {
		t.Fatal(err)
	}
	for name, want := range map[string]string{"scan.pdf": "original", "scan (1).pdf": "previous", "scan (2).pdf": "new"} {
		data, err := os.ReadFile(filepath.Join(dst, name))
		if err != nil || string(data) != want {
			t.Fatalf("%s: %q %v", name, data, err)
		}
	}
	if _, err := os.Stat(input); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("source still exists: %v", err)
	}
}
func TestMovePDFErrors(t *testing.T) {
	for _, scenario := range []string{"missing source", "missing destination", "destination file", "same directory", "directory source", "empty source", "empty destination"} {
		t.Run(scenario, func(t *testing.T) {
			src, dst := t.TempDir(), t.TempDir()
			input := filepath.Join(src, "test.PDF")
			writePDF(t, input, "keep")
			switch scenario {
			case "missing source":
				input = filepath.Join(src, "absent.pdf")
			case "missing destination":
				dst = filepath.Join(dst, "absent")
			case "destination file":
				dst = filepath.Join(dst, "file")
				writePDF(t, dst, "keep")
			case "same directory":
				dst = src
			case "directory source":
				input = src
			case "empty source":
				input = ""
			case "empty destination":
				dst = ""
			}
			if err := NewFileSystem().MovePDF(input, dst); err == nil {
				t.Fatal("expected error")
			}
			data, err := os.ReadFile(filepath.Join(src, "test.PDF"))
			if err != nil || string(data) != "keep" {
				t.Fatal("source changed")
			}
		})
	}
}
func TestMovePDFConcurrent(t *testing.T) {
	dst := t.TempDir()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		src := filepath.Join(t.TempDir(), "same.pdf")
		writePDF(t, src, fmt.Sprintf("pdf %d", i))
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := NewFileSystem().MovePDF(src, dst); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	entries, err := os.ReadDir(dst)
	if err != nil || len(entries) != 12 {
		t.Fatalf("entries=%d err=%v", len(entries), err)
	}
}
func TestGetPDFCancellationAndMissingPath(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewFileSystem().GetPdf(ctx, make(chan *os.File), t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	ctx, cancel = context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	if err := NewFileSystem().GetPdf(ctx, make(chan *os.File), filepath.Join(t.TempDir(), "missing")); err == nil {
		t.Fatal("expected error")
	}
	if err := NewFileSystem().GetPdf(ctx, make(chan *os.File), ""); err == nil {
		t.Fatal("expected error")
	}
}
func TestGetPDFVariations(t *testing.T) {
	root := t.TempDir()
	writePDF(t, filepath.Join(root, "a.PDF"), "%PDF-1.7\nbody\n%%EOF")
	writePDF(t, filepath.Join(root, "ignore.txt"), "text")
	if err := os.Mkdir(filepath.Join(root, "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	writePDF(t, filepath.Join(root, "nested", "ignored.pdf"), "pdf")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	files := make(chan *os.File)
	done := make(chan error, 1)
	go func() { done <- NewFileSystem().GetPdf(ctx, files, root+string(os.PathSeparator)) }()
	select {
	case file := <-files:
		if filepath.Base(file.Name()) != "a.PDF" {
			t.Fatal(file.Name())
		}
		file.Close()
	case <-time.After(5 * time.Second):
		t.Fatal("PDF not found")
	}
	select {
	case file := <-files:
		file.Close()
		t.Fatal("duplicate or ignored file sent")
	case <-time.After(2300 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop")
	}
}
func TestGetPDFBlockedSendCancellation(t *testing.T) {
	root := t.TempDir()
	writePDF(t, filepath.Join(root, "a.pdf"), "%PDF-1.7\nbody\n%%EOF")
	ctx, cancel := context.WithTimeout(context.Background(), 2300*time.Millisecond)
	defer cancel()
	if err := (&FileSystem{pollInterval: 10 * time.Millisecond}).GetPdf(ctx, make(chan *os.File), root); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}
func TestGetPDFReplacement(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "scan.pdf")
	writePDF(t, path, "%PDF-1.7\nfirst\n%%EOF")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	files := make(chan *os.File)
	done := make(chan error, 1)
	fs := &FileSystem{pollInterval: 10 * time.Millisecond}
	go func() { done <- fs.GetPdf(ctx, files, root) }()
	receive := func() {
		t.Helper()
		select {
		case file := <-files:
			file.Close()
		case <-time.After(time.Second):
			t.Fatal("file not delivered")
		}
	}
	receive()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	writePDF(t, path, "%PDF-1.7\nreplacement with different size\n%%EOF")
	receive()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("did not stop")
	}
}
func TestGetPDFReadErrors(t *testing.T) {
	path := filepath.Join(t.TempDir(), "not-directory")
	writePDF(t, path, "pdf")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	fs := &FileSystem{pollInterval: time.Millisecond}
	if err := fs.GetPdf(ctx, make(chan *os.File), path); err == nil {
		t.Fatal("expected directory error")
	}
}
func TestMovePDFSuccessVariations(t *testing.T) {
	for _, name := range []string{"document.PDF", "arquivo com espaços.pdf", "sem-extensao"} {
		t.Run(name, func(t *testing.T) {
			src, dst := t.TempDir(), t.TempDir()
			path := filepath.Join(src, name)
			writePDF(t, path, "content")
			if err := NewFileSystem().MovePDF(path, dst); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(filepath.Join(dst, name))
			if err != nil || string(data) != "content" {
				t.Fatalf("%q %v", data, err)
			}
		})
	}
}
