package domain

import (
	"errors"
	"strings"
)

type ParametersPDF struct {
	ClassificationName string
}

func NewParametersPDF(classificationName string) (*ParametersPDF, error) {
	newClassificationName := strings.ToLower(
		strings.TrimSpace(
			strings.Replace(classificationName, " ", "", -1),
		),
	)

	if newClassificationName == "" {
		return nil, errors.New("invalid classification name")
	}
	switch newClassificationName {
	case "atestado", "carteira_vacinacao", "documento_aluno", "formulario", "outro":
	default:
		return nil, errors.New("invalid classification category")
	}

	return &ParametersPDF{
		ClassificationName: newClassificationName,
	}, nil
}
