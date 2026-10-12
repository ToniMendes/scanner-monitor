package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scanner-monitor/internal/domain"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type transientAI struct {
	calls    atomic.Int32
	failures int32
}

func (a *transientAI) AnalyzePDF(ctx context.Context, f *os.File) (*domain.ParametersPDF, error) {
	if a.calls.Add(1) <= a.failures {
		return nil, errors.New("temporary AI error")
	}
	return &domain.ParametersPDF{ClassificationName: "outro"}, nil
}

type stagedFS struct {
	fakeFS
	folders                atomic.Int32
	moveAttempts           atomic.Int32
	failFolders, failMoves int32
	folder                 string
}

func (f *stagedFS) CreateFolder(category, base string) (string, error) {
	f.folder = base
	if f.folders.Add(1) <= f.failFolders {
		return "", errors.New("temporary mkdir error")
	}
	return base, nil
}
func (f *stagedFS) MovePDF(source, dest string) error {
	if f.moveAttempts.Add(1) <= f.failMoves {
		return errors.New("temporary move error")
	}
	return nil
}
func TestProcessRetriesEachStage(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "scan.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	ai := &transientAI{failures: 2}
	fs := &stagedFS{failFolders: 1, failMoves: 2}
	monitor := NewMonitor(ai, fs)
	monitor.RetryDelay = time.Millisecond
	if err := monitor.process(context.Background(), file, "output"); err != nil {
		t.Fatal(err)
	}
	if ai.calls.Load() != 3 || fs.folders.Load() != 2 || fs.moveAttempts.Load() != 3 {
		t.Fatalf("AI=%d folders=%d moves=%d", ai.calls.Load(), fs.folders.Load(), fs.moveAttempts.Load())
	}
	if fs.folder != "output" {
		t.Fatal(fs.folder)
	}
}
func TestRetryExhaustionAndCancellation(t *testing.T) {
	monitor := NewMonitor(&fakeAI{}, &fakeFS{})
	monitor.RetryDelay = time.Millisecond
	calls := 0
	err := monitor.retry(context.Background(), "test", func(context.Context) error { calls++; return errors.New("persistent") })
	if err == nil || calls != 3 || !strings.Contains(err.Error(), "3 tentativas") {
		t.Fatalf("%d %v", calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err = monitor.retry(ctx, "cancel", func(context.Context) error { cancel(); return errors.New("retry") })
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
func TestFailureDoesNotCancelOtherFiles(t *testing.T) {
	fs := &fakeFS{}
	ai := &fakeAI{err: fmt.Errorf("failure")}
	for i := 0; i < 25; i++ {
		file, err := os.Create(filepath.Join(t.TempDir(), "scan.pdf"))
		if err != nil {
			t.Fatal(err)
		}
		fs.files = append(fs.files, file)
	}
	monitor := NewMonitor(ai, fs)
	monitor.MaxAttempts = 1
	count := 0
	for err := range monitor.Monitoring(context.Background(), "input", "output") {
		if err != nil {
			count++
		}
	}
	if count != 25 || ai.calls.Load() != 25 {
		t.Fatalf("processed=%d errors=%d", ai.calls.Load(), count)
	}
}
func TestRetryCancellationDuringOperation(t *testing.T) {
	monitor := NewMonitor(&fakeAI{}, &fakeFS{})
	ctx, cancel := context.WithCancel(context.Background())
	entered := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- monitor.retry(ctx, "blocked", func(attempt context.Context) error { close(entered); <-attempt.Done(); return attempt.Err() })
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("retry did not stop")
	}
}
func TestRetryReusesClassificationOnMoveFailure(t *testing.T) {
	file, err := os.Create(filepath.Join(t.TempDir(), "scan.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	ai := &transientAI{}
	fs := &stagedFS{failMoves: 2}
	monitor := NewMonitor(ai, fs)
	monitor.RetryDelay = time.Millisecond
	if err := monitor.process(context.Background(), file, "output"); err != nil {
		t.Fatal(err)
	}
	if ai.calls.Load() != 1 || fs.moveAttempts.Load() != 3 {
		t.Fatalf("AI=%d moves=%d", ai.calls.Load(), fs.moveAttempts.Load())
	}
}
