// Package filesystem provides utilities for finding files in directories.
package filesystem

import (
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type FileSystem struct{ pollInterval time.Duration }

func NewFileSystem() *FileSystem {
	return &FileSystem{}
}

func (f *FileSystem) GetPdf(
	ctx context.Context,
	filePDF chan<- *os.File,
	filePath string,
) error {
	if strings.TrimSpace(filePath) == "" {
		return fmt.Errorf("pasta de entrada não informada")
	}
	filePath = filepath.Clean(filePath)

	interval := f.pollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	seen := make(map[string]os.FileInfo)

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
					info, err := d.Info()
					if err != nil {
						return err
					}
					if !info.Mode().IsRegular() {
						return nil
					}

					// Já enviamos este arquivo.
					if previous, ok := seen[path]; ok && os.SameFile(previous, info) && previous.Size() == info.Size() && previous.ModTime().Equal(info.ModTime()) {
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
						seen[path] = info

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

func (f *FileSystem) CreateFolder(classificationName, dirBase string) (string, error) {
	if strings.TrimSpace(dirBase) == "" || classificationName == "" || classificationName == "." || classificationName == ".." || strings.ContainsAny(classificationName, "/\\:") {
		return "", fmt.Errorf("pasta base ou categoria inválida")
	}
	fmt.Println("Criando Pasta.")
	// Caminho da pasta

	dirPath := filepath.Join(
		dirBase,
		classificationName,
	)

	// 0755 define as permissões (no Windows, o SO aplica a ACL padrão do usuário)
	err := os.Mkdir(dirPath, 0755)
	if err != nil {
		if os.IsExist(err) {
			info, statErr := os.Stat(dirPath)
			if statErr != nil {
				return "", statErr
			}
			if !info.IsDir() {
				return "", fmt.Errorf("destino não é um diretório: %q", dirPath)
			}
			fmt.Println("Pasta já existe.")
			return dirPath, nil
		}
		return "", fmt.Errorf("erro ao criar pasta %q: %w", dirPath, err)
	}

	fmt.Println("Pasta criada com sucesso!")
	return dirPath, nil
}

func (f *FileSystem) MovePDF(
	sourcePath string,
	destinationDir string,
) error {
	if strings.TrimSpace(sourcePath) == "" || strings.TrimSpace(destinationDir) == "" {
		return fmt.Errorf("origem e destino são obrigatórios")
	}

	fileName := filepath.Base(sourcePath)

	destinationPath := filepath.Join(
		destinationDir,
		fileName,
	)

	sourceClean, err := filepath.Abs(sourcePath)
	if err != nil {
		return err
	}
	destinationClean, err := filepath.Abs(destinationPath)
	if err != nil {
		return err
	}

	if strings.EqualFold(sourceClean, destinationClean) {
		return fmt.Errorf(
			"arquivo já se encontra no destino: %q",
			sourcePath,
		)
	}

	input, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("erro ao abrir origem: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("origem não é um arquivo regular")
	}
	ext := filepath.Ext(fileName)
	stem := strings.TrimSuffix(fileName, ext)
	for n := 0; ; n++ {
		if n > 0 {
			destinationPath = filepath.Join(destinationDir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		}
		output, err := os.OpenFile(destinationPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("erro ao reservar destino: %w", err)
		}
		_, copyErr := io.Copy(output, input)
		closeErr := output.Close()
		if copyErr != nil || closeErr != nil {
			cleanupErr := os.Remove(destinationPath)
			return fmt.Errorf("erro ao copiar PDF: cópia=%v fechamento=%v limpeza=%v", copyErr, closeErr, cleanupErr)
		}
		if err := input.Close(); err != nil {
			return fmt.Errorf("erro ao fechar origem; cópia preservada: %w", err)
		}
		if err := os.Remove(sourcePath); err != nil {
			return fmt.Errorf("erro ao remover origem; cópia preservada em %q: %w", destinationPath, err)
		}
		return nil
	}
}
