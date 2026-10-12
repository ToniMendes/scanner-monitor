package ai

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"scanner-monitor/internal/domain"
	"testing"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

func classificationJSON(overrides map[string]any) string {
	fields := map[string]any{
		"classification_name": "atestado", "is_blank_form": false,
		"holder_role": "titular", "identification_status": "identificado",
		"holder_name": "João da Silva", "holder_cpf": "529.982.247-25",
		"record_number": "", "identification_evidence": "Página 1: Paciente: João da Silva; CPF: 529.982.247-25",
	}
	for key, value := range overrides {
		fields[key] = value
	}
	data, _ := json.Marshal(fields)
	return string(data)
}

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
		{"success", classificationJSON(nil), 200, 200, false},
		{"upload failure", "", 401, 200, true},
		{"response failure", "", 200, 400, true},
		{"invalid json", "not json", 200, 200, true},
		{"empty classification", classificationJSON(map[string]any{"classification_name": ""}), 200, 200, true},
		{"missing identification", `{"classification_name":"atestado"}`, 200, 200, true},
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
					var request map[string]any
					if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
						t.Error(err)
					}
					if request["instructions"] != classificationInstructions {
						t.Error("classification rules must be trusted instructions")
					}
					input, _ := request["input"].([]any)
					if len(input) != 1 {
						t.Error("expected a single PDF analysis message")
						w.WriteHeader(http.StatusBadRequest)
						return
					}
					message := input[0].(map[string]any)
					fileInput := message["content"].([]any)[0].(map[string]any)
					if fileInput["file_id"] != "file-test" || fileInput["detail"] != "high" {
						t.Errorf("wrong PDF input: %v", fileInput)
					}
					format := request["text"].(map[string]any)["format"].(map[string]any)
					if format["type"] != "json_schema" || format["strict"] != true {
						t.Errorf("expected strict schema: %v", format)
					}
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
			if !tc.bad {
				components, err := got.FolderComponents()
				if err != nil || !reflect.DeepEqual(components, []string{"joaodasilva_52998224725", "atestado"}) {
					t.Fatalf("identification lost: components=%v err=%v", components, err)
				}
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
	data, err := json.Marshal(schema)
	if err != nil {
		t.Fatal(err)
	}
	var parsed struct {
		Type                 string `json:"type"`
		AdditionalProperties *bool  `json:"additionalProperties"`
		Properties           map[string]struct {
			Type string   `json:"type"`
			Enum []string `json:"enum"`
		} `json:"properties"`
		Required []string `json:"required"`
	}
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Type != "object" || parsed.AdditionalProperties == nil || *parsed.AdditionalProperties {
		t.Fatal("schema must forbid additional properties")
	}
	if len(parsed.Properties) != 8 || len(parsed.Required) != len(parsed.Properties) {
		t.Fatalf("schema fields must all be required: %s", data)
	}
	for _, name := range parsed.Required {
		property, ok := parsed.Properties[name]
		if !ok || (property.Type != "string" && property.Type != "boolean") {
			t.Fatalf("invalid required field %q: %+v", name, property)
		}
	}
	// The provider enum and the domain must accept the same document categories.
	for _, category := range parsed.Properties["classification_name"].Enum {
		if _, err := domain.NewParametersPDF(category); err != nil {
			t.Fatalf("schema category %q rejected by domain: %v", category, err)
		}
	}
}

func TestDecodeClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		text string
	}{
		{"legacy response", `{"classification_name":"atestado"}`},
		{"null object", `null`},
		{"null required field", classificationJSON(map[string]any{"is_blank_form": nil})},
		{"wrong boolean type", classificationJSON(map[string]any{"is_blank_form": "false"})},
		{"unknown field", classificationJSON(map[string]any{"destination": "../../escape"})},
		{"unknown category", classificationJSON(map[string]any{"classification_name": "outros"})},
		{"unknown role", classificationJSON(map[string]any{"holder_role": "medico"})},
		{"empty role", classificationJSON(map[string]any{"holder_role": ""})},
		{"unknown status", classificationJSON(map[string]any{"identification_status": "provavel"})},
		{"empty status", classificationJSON(map[string]any{"identification_status": ""})},
		{"trailing object", classificationJSON(nil) + `{}`},
		{"trailing garbage", classificationJSON(nil) + `extra`},
		{"contradictory blank form", classificationJSON(map[string]any{"is_blank_form": true, "classification_name": "formulario"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := decodeClassification(tc.text); err == nil || got != nil {
				t.Fatalf("invalid response accepted: %v %v", got, err)
			}
		})
	}
	for _, tc := range []struct {
		name       string
		overrides  map[string]any
		components []string
	}{
		{"patient", nil, []string{"joaodasilva_52998224725", "atestado"}},
		{"CPF differs from student evidence", map[string]any{"holder_cpf": "111.444.777-35"}, []string{"joaodasilva", "atestado"}},
		{"record differs from student evidence", map[string]any{"holder_cpf": "", "record_number": "000077"}, []string{"joaodasilva", "atestado"}},
		{"confirmed record without CPF", map[string]any{
			"classification_name": "prontuario", "holder_cpf": "", "record_number": "2026/000012",
			"identification_evidence": "Página 1: Nome do aluno: João da Silva; Prontuário: 2026/000012",
		}, []string{"joaodasilva_prontuario_2026-000012"}},
		{"birth certificate", map[string]any{"classification_name": "certidao_nascimento"}, []string{"joaodasilva_52998224725"}},
		{"unreadable vaccination card", map[string]any{
			"classification_name": "carteira_vacinacao", "identification_status": "ilegivel",
			"holder_role": "indeterminado", "holder_name": "", "holder_cpf": "", "identification_evidence": "",
		}, []string{"sem identificação", "carteira_vacinacao"}},
		{"guardian is not student", map[string]any{"classification_name": "identidade", "holder_role": "responsavel"}, []string{"sem identificação", "identidade"}},
		{"conflicting students", map[string]any{"classification_name": "historico_escolar", "identification_status": "ambiguo"}, []string{"sem identificação", "historico_escolar"}},
		{"blank print form", map[string]any{
			"classification_name": "formulario", "is_blank_form": true,
			"holder_role": "indeterminado", "identification_status": "ausente",
			"holder_name": "", "holder_cpf": "", "identification_evidence": "",
		}, []string{"formularios"}},
		{"partial form with no student", map[string]any{
			"classification_name": "formulario", "holder_role": "indeterminado",
			"identification_status": "ausente", "holder_name": "", "holder_cpf": "", "identification_evidence": "",
		}, []string{"sem identificação", "formulario"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := decodeClassification(classificationJSON(tc.overrides))
			if err != nil {
				t.Fatal(err)
			}
			components, err := got.FolderComponents()
			if err != nil || !reflect.DeepEqual(components, tc.components) {
				t.Fatalf("components=%v err=%v; want %v", components, err, tc.components)
			}
		})
	}
}

func TestRejectNonCompletedOrRefusedResponses(t *testing.T) {
	for _, tc := range []struct {
		name, status string
		refused      bool
	}{
		{"incomplete", "incomplete", false},
		{"failed", "failed", false},
		{"missing status", "", false},
		{"refusal", "completed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				content := []any{map[string]any{"type": "output_text", "text": classificationJSON(nil)}}
				if tc.refused {
					content = append(content, map[string]any{"type": "refusal", "refusal": "Cannot process"})
				}
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]any{
					"id": "resp-test", "object": "response", "status": tc.status,
					"output": []any{map[string]any{"type": "message", "role": "assistant", "status": "completed", "content": content}},
				})
			}))
			defer server.Close()
			client := openai.NewClient(option.WithAPIKey("test"), option.WithBaseURL(server.URL+"/"), option.WithMaxRetries(0))
			repo := &OpenAI{Client: &client, modelSDK: "test"}
			if got, err := repo.classifyPDF(context.Background(), "file-test"); err == nil || got != nil {
				t.Fatalf("response accepted: %v %v", got, err)
			}
		})
	}
}
