// Package filesystem provides utilities for finding files in directories.
package filesystem

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileSystem struct{}

func NewFileSystem() *FileSystem {
	return &FileSystem{}
}

func (f *FileSystem) GetPdf(
	ctx context.Context,
	filePDF chan<- *os.File,
) error {

	const filePath = `C:\Users\srden\Downloads\pdfss\`

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	seen := make(map[string]bool)

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()

		case <-ticker.C:
			present := make(map[string]bool)

			err := filepath.WalkDir(
				filePath,
				func(path string, d fs.DirEntry, err error) error {
					if err != nil {
						return err
					}

					// Não entra nas pastas de classificação.
					if d.IsDir() && path != filePath {
						return fs.SkipDir
					}

					if d.IsDir() {
						return nil
					}

					if !strings.EqualFold(
						filepath.Ext(d.Name()),
						".pdf",
					) {
						return nil
					}

					present[path] = true

					// Já enviamos este arquivo.
					if seen[path] {
						return nil
					}

					file, err := os.Open(path)
					if err != nil {
						return fmt.Errorf(
							"erro ao abrir PDF %q: %w",
							path,
							err,
						)
					}

					select {
					case filePDF <- file:
						seen[path] = true

					case <-ctx.Done():
						if err := file.Close(); err != nil {
							return ctx.Err()
						}
						return ctx.Err()
					}

					return nil
				},
			)

			if err != nil {
				return err
			}

			// Se o arquivo saiu da pasta raiz porque foi movido,
			// podemos esquecer dele.
			for path := range seen {
				if !present[path] {
					delete(seen, path)
				}
			}
		}
	}
}

func (f *FileSystem) CreateFolder(classificationName string) (string, error) {
	fmt.Println("Criando Pasta.")
	// Caminho da pasta
	const dirBase = `C:\Users\srden\Downloads\organizados\`

	dirPath := filepath.Join(
		dirBase,
		classificationName,
	)

	// 0755 define as permissões (no Windows, o SO aplica a ACL padrão do usuário)
	err := os.Mkdir(dirPath, 0755)
	if err != nil {
		if os.IsExist(err) {
			fmt.Println("Pasta já existe.")
			return dirPath, nil
		}
		return "", nil
	}

	fmt.Println("Pasta criada com sucesso!")
	return dirPath, nil
}

func (f *FileSystem) MovePDF(
	sourcePath string,
	destinationDir string,
) error {

	fileName := filepath.Base(sourcePath)

	destinationPath := filepath.Join(
		destinationDir,
		fileName,
	)

	sourceClean := filepath.Clean(sourcePath)
	destinationClean := filepath.Clean(destinationPath)

	if strings.EqualFold(sourceClean, destinationClean) {
		return fmt.Errorf(
			"arquivo já se encontra no destino: %q",
			sourcePath,
		)
	}

	if err := os.Rename(
		sourcePath,
		destinationPath,
	); err != nil {

		return fmt.Errorf(
			"erro ao mover %q para %q: %w",
			sourcePath,
			destinationPath,
			err,
		)
	}

	return nil
}
