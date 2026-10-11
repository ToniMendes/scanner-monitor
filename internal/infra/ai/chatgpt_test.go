package ai

import (
	"context"
	"errors"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestNewOpenAI(t *testing.T) {
	for _, tc := range []struct {
		model, key string
		bad        bool
	}{{"", "key", true}, {" \t", "key", true}, {"model", "", true}, {"model", " \n", true}, {"model", "key", false}} {
		got, err := NewOpenAI(tc.model, tc.key)
		if (err != nil) != tc.bad {
			t.Fatalf("%+v: %v", tc, err)
		}
		if !tc.bad && (got.Client == nil || got.modelSDK != tc.model) {
			t.Fatal("invalid client")
		}
	}
}
func TestAnalyzePDFNil(t *testing.T) {
	repo, _ := NewOpenAI("model", "key")
	if _, err := repo.AnalyzePDF(context.Background(), nil); err == nil {
		t.Fatal("expected error")
	}
}
func TestAnalyzePDFResponses(t *testing.T) {
	for _, tc := range []struct {
		name, text                   string
		uploadStatus, responseStatus int
		bad                          bool
	}{
		{"success", `{"classification_name":"atestado"}`, 200, 200, false},
		{"upload failure", "", 401, 200, true},
		{"response failure", "", 200, 400, true},
		{"invalid json", "not json", 200, 200, true},
		{"empty classification", `{"classification_name":""}`, 200, 200, true},
		{"empty output", "", 200, 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/files":
					w.WriteHeader(tc.uploadStatus)
					w.Write([]byte(`{"id":"file-test","object":"file"}`))
				case "/responses":
					w.WriteHeader(tc.responseStatus)
					// Encode output text as JSON so malformed model text remains valid transport JSON.
					writeResponse(w, tc.text)
				default:
					t.Errorf("unexpected path: %s", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer server.Close()
			client := openai.NewClient(option.WithAPIKey("test"), option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))
			repo := &OpenAI{Client: &client, modelSDK: "test"}
			file, err := os.CreateTemp(t.TempDir(), "*.pdf")
			if err != nil {
				t.Fatal(err)
			}
			got, err := repo.AnalyzePDF(context.Background(), file)
			if (err != nil) != tc.bad {
				t.Fatalf("result=%v error=%v", got, err)
			}
			if !tc.bad && got.ClassificationName != "atestado" {
				t.Fatal(got)
			}
			if _, err := file.Read(make([]byte, 1)); !errors.Is(err, os.ErrClosed) {
				t.Fatalf("file not closed: %v", err)
			}
		})
	}
}
func TestUploadClosedFileAndCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unexpected request") }))
	defer server.Close()
	client := openai.NewClient(option.WithAPIKey("test"), option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))
	repo := &OpenAI{Client: &client, modelSDK: "test"}
	if _, err := repo.uploadPDF(context.Background(), nil); err == nil {
		t.Fatal("nil accepted")
	}
	file, err := os.CreateTemp(t.TempDir(), "*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	file.Close()
	if _, err := repo.uploadPDF(context.Background(), file); err == nil {
		t.Fatal("closed file accepted")
	}
	file, err = os.CreateTemp(t.TempDir(), "*.pdf")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.AnalyzePDF(ctx, file); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestGenerateSchema(t *testing.T) {
	schema, err := generateSchema[inputDTO]()
	if err != nil || schema["properties"] == nil || schema["required"] == nil {
		t.Fatalf("%v %v", schema, err)
	}
}
