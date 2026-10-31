package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"

	"scanner-monitor/internal/credential"

	"github.com/openai/openai-go/v3/option"
)

func TestInitializeReplacesRevokedStoredCredential(t *testing.T) {
	var prompts atomic.Int32
	store := &testCredentialStore{key: "revoked"}
	manager := credential.NewManager(store, func(context.Context) (string, error) {
		prompts.Add(1)
		return "replacement", nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			t.Errorf("unexpected validation path %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer replacement" {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[],"object":"list"}`))
	}))
	defer server.Close()

	repo, err := NewOpenAIWithCredentials(context.Background(), "test-model", manager, option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	if repo == nil || store.key != "replacement" || prompts.Load() != 1 {
		t.Fatalf("repo=%v stored=%q prompts=%d", repo, store.key, prompts.Load())
	}
}

func TestConcurrentUnauthorizedUploadsShareOneCredentialRefresh(t *testing.T) {
	const workers = 5
	store := &testCredentialStore{key: "old"}
	var prompts atomic.Int32
	manager := credential.NewManager(store, func(context.Context) (string, error) {
		prompts.Add(1)
		return "new", nil
	})
	var oldUnauthorized, newUploads atomic.Int32
	allOldRequests := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[],"object":"list"}`))
		case "/files":
			if r.Header.Get("Authorization") == "Bearer old" {
				if oldUnauthorized.Add(1) == workers {
					close(allOldRequests)
				}
				<-allOldRequests
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"invalid"}}`))
				return
			}
			payload, err := io.ReadAll(r.Body)
			if err != nil || !bytes.Contains(payload, []byte("full document")) {
				t.Errorf("retried upload did not contain the complete PDF: %v", err)
			}
			newUploads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"file-test","object":"file"}`))
		case "/responses":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "resp", "object": "response", "status": "completed",
				"output": []any{map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": classificationJSON(nil)}}}},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo, err := NewOpenAIWithCredentials(context.Background(), "test-model", manager, option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		file, err := os.CreateTemp(t.TempDir(), "*.pdf")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.WriteString("%PDF-1.7\nfull document\n%%EOF")
		_, _ = file.Seek(0, 0)
		wg.Add(1)
		go func(file *os.File) {
			defer wg.Done()
			_, err := repo.AnalyzePDF(context.Background(), file)
			_ = file.Close()
			errs <- err
		}(file)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if prompts.Load() != 1 || oldUnauthorized.Load() != workers || newUploads.Load() != workers {
		t.Fatalf("prompts=%d old 401s=%d new uploads=%d", prompts.Load(), oldUnauthorized.Load(), newUploads.Load())
	}
}

func TestUnauthorizedClassificationRetriesOnlyTheFailedStage(t *testing.T) {
	store := &testCredentialStore{key: "old"}
	var prompts, uploads, classifications atomic.Int32
	manager := credential.NewManager(store, func(context.Context) (string, error) {
		prompts.Add(1)
		return "new", nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[],"object":"list"}`))
		case "/files":
			uploads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"file-test","object":"file"}`))
		case "/responses":
			if r.Header.Get("Authorization") == "Bearer old" {
				classifications.Add(1)
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":{"message":"revoked"}}`))
				return
			}
			classifications.Add(1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "resp", "object": "response", "status": "completed",
				"output": []any{map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": []any{map[string]any{"type": "output_text", "text": classificationJSON(nil)}}}},
			})
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	repo, err := NewOpenAIWithCredentials(context.Background(), "test-model", manager, option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(t.TempDir(), "*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := repo.AnalyzePDF(context.Background(), file); err != nil {
		t.Fatal(err)
	}
	if prompts.Load() != 1 || uploads.Load() != 1 || classifications.Load() != 2 {
		t.Fatalf("prompts=%d uploads=%d classifications=%d", prompts.Load(), uploads.Load(), classifications.Load())
	}
}

func TestRateLimitDoesNotReplaceCredential(t *testing.T) {
	store := &testCredentialStore{key: "still-valid"}
	var prompts, uploads atomic.Int32
	manager := credential.NewManager(store, func(context.Context) (string, error) {
		prompts.Add(1)
		return "unexpected", nil
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[],"object":"list"}`))
			return
		}
		if r.URL.Path == "/files" {
			uploads.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"slow down","type":"requests","code":"rate_limit_exceeded"}}`))
			return
		}
		t.Errorf("unexpected path %s", r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	repo, err := NewOpenAIWithCredentials(context.Background(), "test-model", manager, option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.CreateTemp(t.TempDir(), "*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	_, err = repo.AnalyzePDF(context.Background(), file)
	var policy retryPolicyError
	if !errors.As(err, &policy) || policy.Retryable() != true {
		t.Fatalf("expected retryable rate-limit error, got %v", err)
	}
	if prompts.Load() != 0 || uploads.Load() != 1 || store.key != "still-valid" {
		t.Fatalf("prompts=%d uploads=%d credential=%q", prompts.Load(), uploads.Load(), store.key)
	}
}

type testCredentialStore struct {
	key string
	err error
}

func (s *testCredentialStore) Load() (string, error) {
	if s.err != nil {
		return "", s.err
	}
	if s.key == "" {
		return "", credential.ErrAPIKeyNotFound
	}
	return s.key, nil
}

func (s *testCredentialStore) Save(key string) error {
	if s.err != nil {
		return s.err
	}
	s.key = key
	return nil
}
