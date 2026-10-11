package domain

import (
	"context"
	"os"
)

type FileSystemRepository interface {
	GetPdf(ctx context.Context, pathChan chan<- *os.File) error
	CreateFolder(string) (string, error)
	MovePDF(string, string) error
}

type GeminiAIRepository interface {
	AnalyzePDF(context.Context, *os.File) (*ParametersPDF, error)
}
