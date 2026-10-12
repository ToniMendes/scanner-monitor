package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"scanner-monitor/internal/domain"
	"scanner-monitor/internal/infra/filesystem"
	"sync/atomic"
	"testing"
	"time"
)

type documentAI struct {
	result *domain.ParametersPDF
	calls  atomic.Int32
}

func (a *documentAI) AnalyzePDF(context.Context, *os.File) (*domain.ParametersPDF, error) {
	a.calls.Add(1)
	return a.result, nil
}

func identifiedDocument(category string) *domain.ParametersPDF {
	return &domain.ParametersPDF{
		ClassificationName:     category,
		HolderName:             "João da Silva",
		HolderCPF:              "529.982.247-25",
		HolderRole:             "aluno",
		IdentificationStatus:   "identificado",
		IdentificationEvidence: "Página 1: Nome do aluno: João da Silva; CPF: 529.982.247-25.",
	}
}

func openScan(t *testing.T) (*os.File, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "scan.pdf")
	if err := os.WriteFile(path, []byte("%PDF-1.7\nconteúdo digitalizado\n%%EOF"), 0600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { file.Close() })
	return file, path
}

func TestProcessOrganizesDocuments(t *testing.T) {
	const person = "joaodasilva_52998224725"
	record := identifiedDocument("prontuario")
	record.HolderCPF = ""
	record.RecordNumber = "0012-A"
	record.IdentificationEvidence = "Página 1: Nome do aluno: João da Silva; Prontuário: 0012-A."
	responsible := identifiedDocument("identidade")
	responsible.HolderRole = "responsavel"
	ambiguous := identifiedDocument("historico_escolar")
	ambiguous.IdentificationStatus = "ambiguo"
	illegible := identifiedDocument("carteira_vacinacao")
	illegible.IdentificationStatus = "ilegivel"
	nameOnly := identifiedDocument("documento_aluno")
	nameOnly.HolderCPF = ""
	for _, tc := range []struct {
		name   string
		result *domain.ParametersPDF
		path   []string
	}{
		{"identity", identifiedDocument("identidade"), []string{person}},
		{"birth certificate", identifiedDocument("certidao_nascimento"), []string{person}},
		{"student document", identifiedDocument("documento_aluno"), []string{person}},
		{"record number", record, []string{"joaodasilva_prontuario_0012-a"}},
		{"vaccination", identifiedDocument("carteira_vacinacao"), []string{person, "carteira_vacinacao"}},
		{"school history", identifiedDocument("historico_escolar"), []string{person, "historico_escolar"}},
		{"medical certificate", identifiedDocument("atestado"), []string{person, "atestado"}},
		{"filled form", identifiedDocument("formulario"), []string{person, "formulario"}},
		{"blank form", &domain.ParametersPDF{ClassificationName: "formulario", IsBlankForm: true}, []string{"formularios"}},
		{"unknown vaccination", &domain.ParametersPDF{ClassificationName: "carteira_vacinacao"}, []string{"sem identificação", "carteira_vacinacao"}},
		{"responsible identity", responsible, []string{"sem identificação", "identidade"}},
		{"ambiguous students", ambiguous, []string{"sem identificação", "historico_escolar"}},
		{"illegible handwriting", illegible, []string{"sem identificação", "carteira_vacinacao"}},
		{"mixed documents", &domain.ParametersPDF{ClassificationName: "documentos_mistos", IdentificationStatus: "ambiguo"}, []string{"sem identificação", "documentos_mistos"}},
		{"name without identifier", nameOnly, []string{"joaodasilva"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, source := openScan(t)
			base := t.TempDir()
			ai := &documentAI{result: tc.result}
			monitor := NewMonitor(ai, filesystem.NewFileSystem())
			monitor.MaxAttempts = 1
			if err := monitor.process(context.Background(), file, base); err != nil {
				t.Fatal(err)
			}
			parts := append([]string{base}, tc.path...)
			destination := filepath.Join(append(parts, "scan.pdf")...)
			data, err := os.ReadFile(destination)
			if err != nil || string(data) != "%PDF-1.7\nconteúdo digitalizado\n%%EOF" {
				t.Fatalf("wrong destination or content: %q, %v", destination, err)
			}
			if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("source was not moved: %v", err)
			}
			if ai.calls.Load() != 1 {
				t.Fatalf("unexpected analysis count: %d", ai.calls.Load())
			}
		})
	}
}

type retryFolderFilesystem struct {
	*filesystem.FileSystem
	requested [][2]string
	failed    bool
}

func (f *retryFolderFilesystem) CreateFolder(component, base string) (string, error) {
	f.requested = append(f.requested, [2]string{component, base})
	if component == "atestado" && !f.failed {
		f.failed = true
		return "", errors.New("temporary failure in document folder")
	}
	return f.FileSystem.CreateFolder(component, base)
}

func TestProcessRestartsFolderCreationFromBase(t *testing.T) {
	file, _ := openScan(t)
	base := t.TempDir()
	ai := &documentAI{result: identifiedDocument("atestado")}
	fs := &retryFolderFilesystem{FileSystem: filesystem.NewFileSystem()}
	monitor := NewMonitor(ai, fs)
	monitor.RetryDelay = time.Millisecond
	if err := monitor.process(context.Background(), file, base); err != nil {
		t.Fatal(err)
	}
	const person = "joaodasilva_52998224725"
	want := [][2]string{{person, base}, {"atestado", filepath.Join(base, person)}, {person, base}, {"atestado", filepath.Join(base, person)}}
	if len(fs.requested) != len(want) {
		t.Fatalf("folder attempts: %v", fs.requested)
	}
	for i := range want {
		if fs.requested[i] != want[i] {
			t.Fatalf("folder attempt %d: %v, expected %v", i, fs.requested[i], want[i])
		}
	}
	if ai.calls.Load() != 1 {
		t.Fatalf("folder retry repeated analysis: %d", ai.calls.Load())
	}
	if _, err := os.Stat(filepath.Join(base, person, "atestado", "scan.pdf")); err != nil {
		t.Fatal(err)
	}
}

func TestProcessPreservesSourceForInvalidAnalysis(t *testing.T) {
	blankWithHolder := identifiedDocument("formulario")
	blankWithHolder.IsBlankForm = true
	for _, result := range []*domain.ParametersPDF{
		{ClassificationName: "../escape"},
		blankWithHolder,
	} {
		file, source := openScan(t)
		base := t.TempDir()
		monitor := NewMonitor(&documentAI{result: result}, filesystem.NewFileSystem())
		monitor.MaxAttempts = 1
		if err := monitor.process(context.Background(), file, base); err == nil {
			t.Fatalf("invalid analysis accepted: %+v", result)
		}
		if _, err := os.Stat(source); err != nil {
			t.Fatalf("source not preserved: %v", err)
		}
		entries, err := os.ReadDir(base)
		if err != nil || len(entries) != 0 {
			t.Fatalf("invalid analysis created destinations: %v, %v", entries, err)
		}
	}
}
