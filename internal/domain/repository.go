package domain

import (
	"context"
	"os"
)

type FileSystemRepository interface {
	GetPdf(ctx context.Context, pathChan chan<- *os.File, filePathBase string) error
	CreateFolder(classificationName, dirBasePath string) (string, error)
	MovePDF(string, string) error
}

type OpenAIRepository interface {
	AnalyzePDF(context.Context, *os.File) (*ParametersPDF, error)
}
