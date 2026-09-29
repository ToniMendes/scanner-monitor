// Package filesystem provides utilities for finding files in directories.
package filesystem

import (
	"io/fs"
	"path/filepath"
	"strings"
)

type FileExplorer struct{}

func NewFileExplorer() *FileExplorer {
	return &FileExplorer{}
}

func (f *FileExplorer) GetPdf(channel chan string) error {
	const filePath = `C:\Users\srden\Downloads\pdfss\`

	walkDirFunc := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".pdf") {
			channel <- path
		}
		return nil
	}

	err := filepath.WalkDir(filePath, walkDirFunc)
	if err != nil {
		return err
	}

	return nil
}
