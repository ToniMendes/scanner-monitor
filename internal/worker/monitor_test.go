package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"scanner-monitor/internal/domain"
	"sync/atomic"
	"testing"
	"time"
)

type fakeAI struct {
	err      error
	nilModel bool
	calls    atomic.Int32
}

func (a *fakeAI) AnalyzePDF(ctx context.Context, f *os.File) (*domain.ParametersPDF, error) {
	a.calls.Add(1)
	if a.err != nil {
		return nil, a.err
	}
	if a.nilModel {
		return nil, nil
	}
	return &domain.ParametersPDF{ClassificationName: "outro"}, nil
}

type fakeFS struct {
	files                       []*os.File
	scanErr, folderErr, moveErr error
	moves                       atomic.Int32
	scans                       atomic.Int32
}

func (f *fakeFS) GetPdf(ctx context.Context, out chan<- *os.File, root string) error {
	if f.scans.Add(1) > 1 {
		return nil
	}
	for _, file := range f.files {
		select {
		case out <- file:
		case <-ctx.Done():
			file.Close()
		}
	}
	return f.scanErr
}
func (f *fakeFS) CreateFolder(category, base string) (string, error) { return base, f.folderErr }
func (f *fakeFS) MovePDF(source, destination string) error           { f.moves.Add(1); return f.moveErr }
func TestMonitor(t *testing.T) {
	sentinel := errors.New("test failure")
	for _, scenario := range []string{"success", "scan error", "ai error", "folder error", "move error", "nil model", "nil file", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			fs := &fakeFS{}
			ai := &fakeAI{}
			count := 1
			if scenario == "success" {
				count = 45
			}
			for i := 0; i < count; i++ {
				file, err := os.Create(filepath.Join(t.TempDir(), "scan.pdf"))
				if err != nil {
					t.Fatal(err)
				}
				fs.files = append(fs.files, file)
				t.Cleanup(func() { file.Close() })
			}
			switch scenario {
			case "scan error":
				fs.scanErr = sentinel
			case "ai error":
				ai.err = sentinel
			case "folder error":
				fs.folderErr = sentinel
			case "move error":
				fs.moveErr = sentinel
			case "nil model":
				ai.nilModel = true
			case "nil file":
				fs.files[0].Close()
				fs.files[0] = nil
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
				// Os arquivos pré-abertos pertencem ao fake; o scanner real
				// não abre arquivos quando o contexto já está cancelado.
				for _, file := range fs.files {
					file.Close()
				}
			}
			monitor := NewMonitor(ai, fs)
			monitor.MaxAttempts = 1
			monitor.RetryDelay = time.Millisecond
			ch := monitor.Monitoring(ctx, "input", "output")
			done := make(chan []error, 1)
			go func() {
				var errs []error
				for err := range ch {
					errs = append(errs, err)
				}
				done <- errs
			}()
			select {
			case errs := <-done:
				wantErr := scenario != "success" && scenario != "cancelled"
				if wantErr && len(errs) != 1 {
					t.Fatalf("errors=%v", errs)
				}
				if !wantErr && len(errs) != 0 {
					t.Fatal(errs)
				}
				if scenario == "success" && (ai.calls.Load() != 45 || fs.moves.Load() != 45) {
					t.Fatalf("calls=%d moves=%d", ai.calls.Load(), fs.moves.Load())
				}
			case <-time.After(3 * time.Second):
				t.Fatal("workers did not terminate")
			}
			for _, file := range fs.files {
				if file != nil {
					if _, err := file.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
						t.Fatalf("file leaked: %v", err)
					}
				}
			}
		})
	}
}
