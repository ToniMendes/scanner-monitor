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

	return &ParametersPDF{
		ClassificationName: newClassificationName,
	}, nil
}
