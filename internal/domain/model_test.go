package domain

import "testing"

func TestNewParametersPDF(t *testing.T) {
	for _, value := range []string{"", " ", "\t\n", "   \r\n"} {
		if got, err := NewParametersPDF(value); err == nil || got != nil {
			t.Fatalf("%q accepted", value)
		}
	}
	for _, value := range []string{"atestado", "outro", " carteira_vacinacao ", "DOCUMENTO_ALUNO"} {
		got, err := NewParametersPDF(value)
		if err != nil || got.ClassificationName != value {
			t.Fatalf("%q: %v %v", value, got, err)
		}
	}
}
