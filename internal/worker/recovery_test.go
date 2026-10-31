package worker

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"scanner-monitor/internal/domain"
	"strings"
	"sync"
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
	if ai.calls.Load() != 3 || fs.folders.Load() != 3 || fs.moveAttempts.Load() != 3 {
		t.Fatalf("AI=%d folders=%d moves=%d", ai.calls.Load(), fs.folders.Load(), fs.moveAttempts.Load())
	}
	if fs.folder != "output" {
		t.Fatal(fs.folder)
	}
}

type readingAI struct {
	calls atomic.Int32
	mu    sync.Mutex
	data  [][]byte
}

func (a *readingAI) AnalyzePDF(_ context.Context, file *os.File) (*domain.ParametersPDF, error) {
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	a.data = append(a.data, content)
	a.mu.Unlock()
	if a.calls.Add(1) == 1 {
		return nil, errors.New("temporary upload failure")
	}
	return &domain.ParametersPDF{ClassificationName: "outro"}, nil
}

func TestPDFRetryReopensFullContent(t *testing.T) {
	const content = "%PDF-1.7\ncomplete document contents\n%%EOF"
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ai := &readingAI{}
	monitor := NewMonitor(ai, &stagedFS{})
	monitor.MaxAttempts = 2
	monitor.RetryDelay = time.Millisecond
	if err := monitor.process(context.Background(), file, "output"); err != nil {
		t.Fatal(err)
	}
	if ai.calls.Load() != 2 || len(ai.data) != 2 || string(ai.data[0]) != content || string(ai.data[1]) != content {
		t.Fatalf("calls=%d contents=%q", ai.calls.Load(), ai.data)
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

type policyError struct {
	retry bool
	delay time.Duration
}

func (e policyError) Error() string             { return "classified API failure" }
func (e policyError) Retryable() bool           { return e.retry }
func (e policyError) RetryAfter() time.Duration { return e.delay }

func TestRetryStopsForPermanentAPIError(t *testing.T) {
	monitor := NewMonitor(&fakeAI{}, &fakeFS{})
	calls := 0
	err := monitor.retry(context.Background(), "forbidden", func(context.Context) error {
		calls++
		return policyError{retry: false}
	})
	if err == nil || calls != 1 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

func TestRetryLimitsTransientAPIError(t *testing.T) {
	monitor := NewMonitor(&fakeAI{}, &fakeFS{})
	monitor.RetryDelay = time.Millisecond
	calls := 0
	err := monitor.retry(context.Background(), "server", func(context.Context) error {
		calls++
		return policyError{retry: true}
	})
	if err == nil || calls != 3 {
		t.Fatalf("calls=%d error=%v", calls, err)
	}
}

type blockingAI struct{ started chan struct{} }

func (a *blockingAI) AnalyzePDF(ctx context.Context, _ *os.File) (*domain.ParametersPDF, error) {
	select {
	case <-a.started:
	default:
		close(a.started)
	}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestShutdownDuringProcessingPreservesPDF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "scan.pdf")
	const content = "%PDF-1.7\nkeep this document\n%%EOF"
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ai := &blockingAI{started: make(chan struct{})}
	monitor := NewMonitor(ai, &fakeFS{files: []*os.File{file}})
	ctx, cancel := context.WithCancel(context.Background())
	errCh := monitor.Monitoring(ctx, "input", "output")
	select {
	case <-ai.started:
		cancel()
	case <-time.After(time.Second):
		cancel()
		t.Fatal("worker did not start processing")
	}
	done := make(chan struct{})
	go func() {
		for range errCh {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("monitor did not stop after cancellation")
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != content {
		t.Fatalf("PDF not preserved during shutdown: %q %v", got, err)
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
