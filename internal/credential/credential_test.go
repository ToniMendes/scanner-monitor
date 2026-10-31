package credential

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
)

type memoryStore struct {
	mu      sync.Mutex
	key     string
	err     error
	saveErr error
}

func (s *memoryStore) Load() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return "", s.err
	}
	if s.key == "" {
		return "", ErrAPIKeyNotFound
	}
	return s.key, nil
}

func (s *memoryStore) Save(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.saveErr != nil {
		return s.saveErr
	}
	s.key = key
	return nil
}

func TestInitializeValidatesBeforePersistingPromptedCredential(t *testing.T) {
	store := &memoryStore{}
	manager := NewManager(store, func(context.Context) (string, error) { return "valid", nil })
	var validationCalls atomic.Int32
	snapshot, err := manager.Initialize(context.Background(), func(_ context.Context, key string) error {
		validationCalls.Add(1)
		if key != "valid" {
			t.Fatalf("validated unexpected key %q", key)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Key != "valid" || snapshot.Generation == 0 || validationCalls.Load() != 1 {
		t.Fatalf("snapshot=%+v validations=%d", snapshot, validationCalls.Load())
	}
	if store.key != "valid" {
		t.Fatal("validated key was not persisted")
	}
}

func TestInitializeKeepsStoredCredentialOnTemporaryValidationFailure(t *testing.T) {
	store := &memoryStore{key: "stored"}
	prompted := false
	manager := NewManager(store, func(context.Context) (string, error) { prompted = true; return "replacement", nil })
	temporary := errors.New("network unavailable")
	_, err := manager.Initialize(context.Background(), func(context.Context, string) error { return temporary })
	if !errors.Is(err, temporary) {
		t.Fatalf("unexpected error: %v", err)
	}
	if prompted || store.key != "stored" {
		t.Fatalf("prompted=%v stored=%q", prompted, store.key)
	}
}

func TestConcurrentRefreshPromptsOnceForFailedGeneration(t *testing.T) {
	store := &memoryStore{key: "old"}
	var promptCalls atomic.Int32
	manager := NewManager(store, func(context.Context) (string, error) {
		promptCalls.Add(1)
		return "new", nil
	})
	validate := func(context.Context, string) error { return nil }
	initial, err := manager.Initialize(context.Background(), validate)
	if err != nil {
		t.Fatal(err)
	}

	const workers = 5
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			got, err := manager.Refresh(context.Background(), initial.Generation, validate)
			if err == nil && got.Key != "new" {
				err = errors.New("worker did not receive renewed credential")
			}
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if promptCalls.Load() != 1 {
		t.Fatalf("prompt called %d times", promptCalls.Load())
	}
}

func TestInvalidReplacementDoesNotOverwriteStoredCredential(t *testing.T) {
	store := &memoryStore{key: "old"}
	manager := NewManager(store, func(context.Context) (string, error) { return "bad", nil })
	initial, err := manager.Initialize(context.Background(), func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	_, err = manager.Refresh(context.Background(), initial.Generation, func(_ context.Context, key string) error {
		if key == "bad" {
			return ErrInvalidCredential
		}
		return nil
	})
	if !errors.Is(err, ErrInvalidCredential) || store.key != "old" {
		t.Fatalf("error=%v stored=%q", err, store.key)
	}
}

func TestSecondRevocationCanRefreshAfterFirstReplacement(t *testing.T) {
	store := &memoryStore{key: "old"}
	var prompts atomic.Int32
	manager := NewManager(store, func(context.Context) (string, error) {
		return fmt.Sprintf("new-%d", prompts.Add(1)), nil
	})
	validate := func(context.Context, string) error { return nil }
	first, err := manager.Initialize(context.Background(), validate)
	if err != nil {
		t.Fatal(err)
	}
	second, err := manager.Refresh(context.Background(), first.Generation, validate)
	if err != nil {
		t.Fatal(err)
	}
	third, err := manager.Refresh(context.Background(), second.Generation, validate)
	if err != nil {
		t.Fatal(err)
	}
	if second.Key != "new-1" || third.Key != "new-2" || third.Generation <= second.Generation {
		t.Fatalf("first refresh=%+v second refresh=%+v", second, third)
	}
}

func TestSaveFailureDoesNotPublishReplacement(t *testing.T) {
	store := &memoryStore{key: "old"}
	manager := NewManager(store, func(context.Context) (string, error) { return "new", nil })
	initial, err := manager.Initialize(context.Background(), func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	saveErr := errors.New("keyring unavailable")
	store.saveErr = saveErr
	if _, err := manager.Refresh(context.Background(), initial.Generation, func(context.Context, string) error { return nil }); !errors.Is(err, saveErr) {
		t.Fatalf("unexpected save error: %v", err)
	}
	current, err := manager.Refresh(context.Background(), initial.Generation, func(context.Context, string) error { return nil })
	if err == nil || current.Key != "" || store.key != "old" {
		t.Fatalf("failed save published a key: snapshot=%+v stored=%q error=%v", current, store.key, err)
	}
}

func TestCancellationDuringRenewalLeavesStoredCredentialAndUnblocks(t *testing.T) {
	store := &memoryStore{key: "old"}
	entered := make(chan struct{})
	manager := NewManager(store, func(ctx context.Context) (string, error) {
		close(entered)
		<-ctx.Done()
		return "", ctx.Err()
	})
	initial, err := manager.Initialize(context.Background(), func(context.Context, string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := manager.Refresh(ctx, initial.Generation, func(context.Context, string) error { return nil })
		done <- err
	}()
	<-entered
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("renewal returned %v", err)
	}
	if store.key != "old" {
		t.Fatalf("cancelled renewal changed stored credential to %q", store.key)
	}
}
