package domain

import (
	"reflect"
	"strings"
	"testing"
)

func TestNewParametersPDF(t *testing.T) {
	for _, value := range []string{"", " ", "\t\n", "   \r\n", "unknown", "../outro", "a/b"} {
		if got, err := NewParametersPDF(value); err == nil || got != nil {
			t.Fatalf("%q accepted", value)
		}
	}
	for _, value := range []string{"atestado", "outro", " carteira_vacinacao ", "DOCUMENTO_ALUNO"} {
		got, err := NewParametersPDF(value)
		if err != nil || got.ClassificationName != strings.ToLower(strings.TrimSpace(value)) {
			t.Fatalf("%q: %v %v", value, got, err)
		}
	}
}

func identifiedDocument(category string) *ParametersPDF {
	return &ParametersPDF{
		ClassificationName:     category,
		HolderName:             "João da Silva",
		HolderCPF:              "529.982.247-25",
		HolderRole:             "aluno",
		IdentificationStatus:   "identificado",
		IdentificationEvidence: "Aluno: João da Silva, CPF: 529.982.247-25",
	}
}

func TestFolderComponents(t *testing.T) {
	for _, category := range []string{"identidade", "certidao_nascimento", "prontuario", "documento_aluno"} {
		t.Run(category, func(t *testing.T) {
			assertFolder(t, identifiedDocument(category), []string{"joaodasilva_52998224725"})
		})
	}
	for _, category := range []string{"carteira_vacinacao", "historico_escolar", "atestado", "formulario", "outro", "documentos_mistos"} {
		t.Run(category, func(t *testing.T) {
			assertFolder(t, identifiedDocument(category), []string{"joaodasilva_52998224725", category})
		})
	}
	t.Run("blank print template", func(t *testing.T) {
		assertFolder(t, &ParametersPDF{ClassificationName: "formulario", IsBlankForm: true}, []string{"formularios"})
	})
	t.Run("filled form with no readable holder is not print template", func(t *testing.T) {
		assertFolder(t, &ParametersPDF{ClassificationName: "formulario", IdentificationStatus: "ilegivel"}, []string{"sem identificação", "formulario"})
	})
	t.Run("legacy category only is unidentified", func(t *testing.T) {
		pdf, err := NewParametersPDF("carteira_vacinacao")
		if err != nil {
			t.Fatal(err)
		}
		if pdf.HolderRole != "indeterminado" || pdf.IdentificationStatus != "ausente" {
			t.Fatalf("unexpected legacy defaults: %#v", pdf)
		}
		assertFolder(t, pdf, []string{"sem identificação", "carteira_vacinacao"})
	})
	t.Run("record number preserves zeroes and normalizes separators", func(t *testing.T) {
		pdf := identifiedDocument("prontuario")
		pdf.HolderCPF = ""
		pdf.RecordNumber = " AB_000012 - 2026 "
		pdf.IdentificationEvidence += "; Prontuário: AB_000012 - 2026"
		assertFolder(t, pdf, []string{"joaodasilva_prontuario_ab-000012-2026"})
	})
	for _, record := range []struct{ value, component string }{
		{"2026/0012", "2026-0012"},
		{"AB/000012", "ab-000012"},
		{"  AB / 000012  ", "ab-000012"},
	} {
		t.Run("printed record "+record.value, func(t *testing.T) {
			pdf := identifiedDocument("prontuario")
			pdf.HolderCPF = ""
			pdf.RecordNumber = record.value
			pdf.IdentificationEvidence += "; Prontuário: " + record.value
			assertFolder(t, pdf, []string{"joaodasilva_prontuario_" + record.component})
		})
	}
	t.Run("valid CPF takes priority over record", func(t *testing.T) {
		pdf := identifiedDocument("identidade")
		pdf.RecordNumber = "000012"
		pdf.IdentificationEvidence += "; Prontuário: 000012"
		assertFolder(t, pdf, []string{"joaodasilva_52998224725"})
	})
	t.Run("identified name without document number", func(t *testing.T) {
		pdf := identifiedDocument("atestado")
		pdf.HolderCPF = ""
		assertFolder(t, pdf, []string{"joaodasilva", "atestado"})
	})
	t.Run("accents and compound surnames", func(t *testing.T) {
		pdf := identifiedDocument("identidade")
		pdf.HolderName = "  Ana-Mária D’Ávila  "
		pdf.HolderRole = "titular"
		pdf.IdentificationEvidence = "Titular: Ana-Mária D'Ávila. CPF: 529.982.247-25"
		assertFolder(t, pdf, []string{"anamariadavila_52998224725"})
	})
}

func TestHolderDataCannotRouteGuardianOrUncertainIdentity(t *testing.T) {
	for _, role := range []string{"responsavel", "indeterminado"} {
		t.Run(role, func(t *testing.T) {
			pdf := identifiedDocument("identidade")
			pdf.HolderRole = role
			pdf.RecordNumber = "000012"
			normalized, err := pdf.Normalize()
			if err != nil {
				t.Fatal(err)
			}
			if normalized.HolderName != "" || normalized.HolderCPF != "" || normalized.RecordNumber != "" || normalized.IdentificationEvidence != "" {
				t.Fatalf("unsafe holder data retained: %#v", normalized)
			}
			wantStatus := "ausente"
			if role == "indeterminado" {
				wantStatus = "ambiguo"
			}
			if normalized.IdentificationStatus != wantStatus {
				t.Fatalf("status = %q; want %q", normalized.IdentificationStatus, wantStatus)
			}
			assertFolder(t, normalized, []string{"sem identificação", "identidade"})
		})
	}
	for _, status := range []string{"ausente", "ilegivel", "ambiguo"} {
		t.Run(status, func(t *testing.T) {
			pdf := identifiedDocument("carteira_vacinacao")
			pdf.IdentificationStatus = status
			assertFolder(t, pdf, []string{"sem identificação", "carteira_vacinacao"})
		})
	}
	t.Run("no explicit source evidence", func(t *testing.T) {
		pdf := identifiedDocument("identidade")
		pdf.IdentificationEvidence = " \n "
		assertFolder(t, pdf, []string{"sem identificação", "identidade"})
	})
	for _, name := range []string{
		"", "João", "Ana-Maria", "João S", "João S.", "Aluno Não Identificado", "Nome Desconhecido", "Nome Ilegível",
		"Nome Completo", "Nome do Aluno", "Nome do Titular", "Assinatura Responsável", "Aluno João Silva",
		"../../João Silva", `João\Silva`, "João: Silva", "João ? Silva", "João 5ilva", "João\x00 Silva", "João\nSilva",
		strings.Repeat("Joao", 31) + " Silva",
	} {
		t.Run(name, func(t *testing.T) {
			pdf := identifiedDocument("identidade")
			pdf.HolderName = name
			assertFolder(t, pdf, []string{"sem identificação", "identidade"})
		})
	}
}

func TestEvidenceMustContainTheCompleteHolderName(t *testing.T) {
	for _, evidence := range []string{
		"Nome legível no documento", "Aluno: Pedro da Silva", "Aluno: João Silva", "Aluno: João da Silvana", "Aluno: João",
	} {
		t.Run(evidence, func(t *testing.T) {
			pdf := identifiedDocument("identidade")
			pdf.IdentificationEvidence = evidence
			normalized, err := pdf.Normalize()
			if err != nil {
				t.Fatal(err)
			}
			if normalized.IdentificationEvidence != "" || normalized.IdentificationStatus != "ambiguo" {
				t.Fatalf("conflicting evidence retained: %#v", normalized)
			}
			assertFolder(t, pdf, []string{"sem identificação", "identidade"})
		})
	}
	pdf := identifiedDocument("identidade")
	pdf.IdentificationEvidence = "ALUNO: JOAO\nDA SILVA; CPF: 52998224725"
	assertFolder(t, pdf, []string{"joaodasilva_52998224725"})
}

func TestCPFVerificationNeverRepairsOCR(t *testing.T) {
	for _, cpf := range []string{
		"", "52998224724", "11111111111", "00000000000", "5299822472", "529982247255",
		"529.982.247-2S", "529.982.247/25", "CPF 529.982.247-25", "529\n98224725", "５２９９８２２４７２５",
	} {
		t.Run(cpf, func(t *testing.T) {
			pdf := identifiedDocument("identidade")
			pdf.HolderCPF = cpf
			pdf.RecordNumber = "000012"
			pdf.IdentificationEvidence += "; Prontuário: 000012"
			normalized, err := pdf.Normalize()
			if err != nil {
				t.Fatal(err)
			}
			if normalized.HolderCPF != "" {
				t.Fatalf("invalid CPF accepted: %q", normalized.HolderCPF)
			}
			assertFolder(t, pdf, []string{"joaodasilva_prontuario_000012"})
		})
	}
	for _, cpf := range []string{"52998224725", " 529.982.247-25 ", "529 982 247 25", "01234567890"} {
		t.Run("valid "+cpf, func(t *testing.T) {
			pdf := identifiedDocument("identidade")
			pdf.HolderCPF = cpf
			pdf.IdentificationEvidence = "Aluno: João da Silva; CPF: " + cpf
			normalized, err := pdf.Normalize()
			if err != nil {
				t.Fatal(err)
			}
			if len(normalized.HolderCPF) != 11 {
				t.Fatalf("valid CPF rejected: %q", normalized.HolderCPF)
			}
		})
	}
}

func TestRecordNumberRejectsPathsAndPlaceholders(t *testing.T) {
	for _, record := range []string{
		"../000012", "../../", `C:\000012`, `000012\subdir`, "/000012", "000012/", "AB//000012", "AB/ /000012",
		"AB/000012/ - ", "000.012", "000012:stream",
		"000012\x00", "000012\n123", "CON", "COM1", "LPT9", "não informado", "desconhecido", "___",
		strings.Repeat("0", 41),
	} {
		t.Run(record, func(t *testing.T) {
			pdf := identifiedDocument("identidade")
			pdf.HolderCPF = ""
			pdf.RecordNumber = record
			pdf.IdentificationEvidence += "; Prontuário: " + record
			assertFolder(t, pdf, []string{"joaodasilva"})
		})
	}
}

func TestOnlyIdentifiersSupportedByHolderEvidenceAreUsed(t *testing.T) {
	const student = "Página 1: Nome do aluno: João da Silva; "
	cases := []struct {
		name, cpf, record, evidence, folder string
	}{
		{"guardian CPF differs from student evidence", "111.444.777-35", "000012", student + "CPF: 529.982.247-25; Prontuário: 000012", "joaodasilva_prontuario_000012"},
		{"divergent CPF without fallback record", "111.444.777-35", "", student + "CPF: 529.982.247-25", "joaodasilva"},
		{"formatted CPF against digits evidence", "529.982.247-25", "", student + "CPF: 52998224725", "joaodasilva_52998224725"},
		{"CPF digits against formatted evidence", "52998224725", "", student + "CPF: 529.982.247-25", "joaodasilva_52998224725"},
		{"CPF without proper label", "52998224725", "", student + "Cartão: 52998224725", "joaodasilva"},
		{"CPF substring of larger number", "52998224725", "", student + "CPF: 152998224725", "joaodasilva"},
		{"CPF substring of larger formatted number", "52998224725", "", student + "CPF: 529.982.247-25-0", "joaodasilva"},
		{"CPF next to letter is not entire value", "52998224725", "", student + "CPF: 52998224725S", "joaodasilva"},
		{"divergent record", "", "000013", student + "Prontuário: 000012", "joaodasilva"},
		{"record substring of larger number", "", "0012", student + "Prontuário: 000012", "joaodasilva"},
		{"record suffix of larger identifier", "", "0012", student + "Prontuário: AB/0012", "joaodasilva"},
		{"record prefix of larger identifier", "", "0012", student + "Prontuário: 0012/2026", "joaodasilva"},
		{"record prefix before numeric dot separator", "", "0012", student + "Prontuário: 0012.2026", "joaodasilva"},
		{"record prefix before number separated by space", "", "0012", student + "Prontuário: 0012 2026", "joaodasilva"},
		{"record prefix before number with spaced separator", "", "0012", student + "Prontuário: 0012 / 2026", "joaodasilva"},
		{"page number is not record evidence", "", "1", "Página 1: Nome do aluno: João da Silva", "joaodasilva"},
		{"unlabeled number is not record evidence", "", "000012", student + "000012", "joaodasilva"},
		{"record formatting agrees with evidence", "", "AB_000012", student + "Prontuário: AB / 000012", "joaodasilva_prontuario_ab-000012"},
		{"record with sentence punctuation", "", "0012-A", student + "Prontuário: 0012-A.", "joaodasilva_prontuario_0012-a"},
		{"record with punctuation before sentence", "", "0012-A", student + "Prontuário: 0012-A. Documento escolar.", "joaodasilva_prontuario_0012-a"},
		{"unsupported record does not discard CPF", "52998224725", "000013", student + "CPF: 52998224725; Prontuário: 000012", "joaodasilva_52998224725"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			pdf := identifiedDocument("identidade")
			pdf.HolderCPF = test.cpf
			pdf.RecordNumber = test.record
			pdf.IdentificationEvidence = test.evidence
			assertFolder(t, pdf, []string{test.folder})
		})
	}
}

func TestCommonStudentIdentifierLabels(t *testing.T) {
	for _, label := range []string{
		"CPF nº:", "CPF N°:", "CPF Número:", "CPF n.º:", "CPF do aluno:", "CPF da aluna:",
		"CPF do titular:", "CPF do paciente:", "CPF do estudante:", "CPF do aluno Nº:", "CPF nº do titular:",
	} {
		t.Run(label, func(t *testing.T) {
			pdf := identifiedDocument("identidade")
			pdf.IdentificationEvidence = "Nome do aluno: João da Silva; " + label + " 529.982.247-25"
			assertFolder(t, pdf, []string{"joaodasilva_52998224725"})
		})
	}
	for _, label := range []string{
		"Prontuário nº:", "PRONTUÁRIO N°:", "Prontuário Número:", "Prontuário n.º:", "Prontuário do aluno:",
		"Prontuário do titular:", "Prontuário do paciente:", "Prontuário do estudante:", "Prontuário do aluno Nº:",
	} {
		t.Run(label, func(t *testing.T) {
			pdf := identifiedDocument("prontuario")
			pdf.HolderCPF = ""
			pdf.RecordNumber = "0012"
			pdf.IdentificationEvidence = "Nome do aluno: João da Silva; " + label + " 0012"
			assertFolder(t, pdf, []string{"joaodasilva_prontuario_0012"})
		})
	}
	for _, person := range []string{"do responsável", "da mãe", "do pai"} {
		t.Run("third party "+person, func(t *testing.T) {
			pdf := identifiedDocument("identidade")
			pdf.RecordNumber = "0012"
			pdf.IdentificationEvidence = "Nome do aluno: João da Silva; CPF " + person + ": 529.982.247-25; Prontuário " + person + ": 0012"
			assertFolder(t, pdf, []string{"joaodasilva"})
		})
	}
}

func TestNormalizeRejectsInvalidSemantics(t *testing.T) {
	cases := []*ParametersPDF{
		nil,
		{ClassificationName: "../identidade"},
		{ClassificationName: "outro", HolderRole: "pai"},
		{ClassificationName: "outro", IdentificationStatus: "talvez"},
		{ClassificationName: "identidade", IsBlankForm: true},
		{ClassificationName: "formulario", IsBlankForm: true, HolderName: "João da Silva"},
		{ClassificationName: "formulario", IsBlankForm: true, HolderCPF: "52998224725"},
		{ClassificationName: "formulario", IsBlankForm: true, RecordNumber: "000012"},
		{ClassificationName: "formulario", IsBlankForm: true, IdentificationStatus: "identificado"},
		{ClassificationName: "formulario", IsBlankForm: true, HolderRole: "aluno"},
		{ClassificationName: "formulario", IsBlankForm: true, IdentificationEvidence: "Nome preenchido"},
	}
	for index, pdf := range cases {
		if normalized, err := pdf.Normalize(); err == nil || normalized != nil {
			t.Errorf("case %d accepted: %#v", index, normalized)
		}
		if folders, err := pdf.FolderComponents(); err == nil || folders != nil {
			t.Errorf("case %d routed: %#v", index, folders)
		}
	}
}

func TestNormalizeCopyAndIdempotence(t *testing.T) {
	pdf := identifiedDocument(" IDENTIDADE ")
	pdf.HolderRole = " ALUNO "
	pdf.IdentificationStatus = " IDENTIFICADO "
	pdf.HolderName = "  João   da  Silva  "
	pdf.RecordNumber = " AB_00012 "
	pdf.IdentificationEvidence += "; Prontuário: AB_00012"
	original := *pdf
	first, err := pdf.Normalize()
	if err != nil {
		t.Fatal(err)
	}
	if *pdf != original {
		t.Fatal("Normalize mutated input")
	}
	second, err := first.Normalize()
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("Normalize is not idempotent: %#v -> %#v (%v)", first, second, err)
	}
	if first.HolderName != "João da Silva" || first.RecordNumber != "ab-00012" {
		t.Fatalf("unexpected normalized fields: %#v", first)
	}
}

func assertFolder(t *testing.T, pdf *ParametersPDF, want []string) {
	t.Helper()
	got, err := pdf.FolderComponents()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("folders = %#v; want %#v", got, want)
	}
}
