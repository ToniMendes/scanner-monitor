// Package filesystem provides utilities for finding files in directories.
package filesystem

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type FileSystem struct {
	pollInterval time.Duration
	publish      func(string, string) error
	removeSource func(string, os.FileInfo) error
}

var moveGate = make(chan struct{}, 1)
var ErrDuplicate = errors.New("PDF idêntico já existe no destino; origem preservada para revisão")

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
	observed := make(map[string]os.FileInfo)

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
					previous, stable := observed[path]
					observed[path] = info
					if !stable || !os.SameFile(previous, info) || previous.Size() != info.Size() || !previous.ModTime().Equal(info.ModTime()) || info.Size() == 0 {
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

					if !completePDF(file, info.Size()) {
						file.Close()
						return nil
					}
					select {
					// Tamanho estável e marcadores PDF são uma heurística,
					// não uma validação completa do formato.
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
			for path := range observed {
				if !present[path] {
					delete(observed, path)
				}
			}
		}
	}
}

func (f *FileSystem) CreateFolder(classificationName, dirBase string) (string, error) {
	if strings.TrimSpace(dirBase) == "" || !validFolderComponent(classificationName) {
		return "", fmt.Errorf("pasta base ou categoria inválida")
	}
	dirPath := filepath.Join(dirBase, classificationName)
	absBase, err := filepath.Abs(dirBase)
	if err != nil {
		return "", fmt.Errorf("erro ao resolver pasta base: %w", err)
	}
	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		return "", fmt.Errorf("erro ao resolver pasta de destino: %w", err)
	}
	relativePath, err := filepath.Rel(absBase, absPath)
	if err != nil || relativePath != classificationName {
		return "", fmt.Errorf("pasta de destino fora da pasta base")
	}

	// 0755 define as permissões (no Windows, o SO aplica a ACL padrão do usuário)
	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return "", fmt.Errorf("erro ao criar pasta %q: %w", dirPath, err)
	}
	info, err := os.Lstat(dirPath)
	if err != nil {
		return "", fmt.Errorf("erro ao verificar pasta %q: %w", dirPath, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", fmt.Errorf("destino não é um diretório regular: %q", dirPath)
	}
	fmt.Printf("Pasta de destino preparada: %q.\n", dirPath)
	return dirPath, nil
}

// validFolderComponent mantém o destino em um único nível e compatível com Windows.
func validFolderComponent(name string) bool {
	if strings.TrimSpace(name) == "" || name == "." || name == ".." || strings.ContainsAny(name, `<>:"/\|?*`) || strings.HasSuffix(name, ".") || strings.HasSuffix(name, " ") {
		return false
	}
	units := 0
	for _, char := range name {
		if unicode.IsControl(char) {
			return false
		}
		units++
		if char > 0xffff {
			units++
		}
	}
	if units > 255 {
		return false
	}
	stem, _, _ := strings.Cut(name, ".")
	stem = strings.ToUpper(strings.TrimRight(stem, " "))
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$", "CONIN$", "CONOUT$":
		return false
	}
	if strings.HasPrefix(stem, "COM") || strings.HasPrefix(stem, "LPT") {
		suffix := stem[3:]
		if len([]rune(suffix)) == 1 && strings.ContainsAny(suffix, "123456789¹²³") {
			return false
		}
	}
	return true
}

func (f *FileSystem) MovePDF(
	sourcePath string,
	destinationDir string,
) error {
	return f.MovePDFContext(context.Background(), sourcePath, destinationDir)
}

// MovePDFContext publica apenas cópias completas, sem sobrescrever o destino.
func (f *FileSystem) MovePDFContext(ctx context.Context, sourcePath, destinationDir string) error {
	select {
	case moveGate <- struct{}{}:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { <-moveGate }()
	if err := ctx.Err(); err != nil {
		return err
	}
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
	temp, err := os.CreateTemp(destinationDir, ".scanner-*.partial")
	if err != nil {
		return fmt.Errorf("erro ao criar temporário: %w", err)
	}
	defer func() { temp.Close(); os.Remove(temp.Name()) }()
	hash := sha256.New()
	if _, err := io.Copy(io.MultiWriter(temp, hash), &contextReader{ctx: ctx, reader: input}); err != nil {
		return fmt.Errorf("erro ao copiar PDF: %w", err)
	}
	if err := temp.Sync(); err != nil {
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	after, err := input.Stat()
	if err != nil {
		return err
	}
	if after.Size() != info.Size() || !after.ModTime().Equal(info.ModTime()) {
		return errors.New("origem alterada durante a cópia; tente novamente quando estiver estável")
	}
	if err := input.Close(); err != nil {
		return err
	}
	entries, err := os.ReadDir(destinationDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.EqualFold(filepath.Ext(entry.Name()), ".pdf") {
			continue
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if !entryInfo.Mode().IsRegular() {
			continue
		}
		candidate := filepath.Join(destinationDir, entry.Name())
		identical, err := sameContent(ctx, candidate, info.Size(), hash.Sum(nil))
		if err != nil {
			return err
		}
		if identical {
			fmt.Printf("[PDF %q] Conteúdo idêntico encontrado em %q; origem preservada.\n", sourcePath, candidate)
			return fmt.Errorf("%w: %s", ErrDuplicate, candidate)
		}
	}
	ext := filepath.Ext(fileName)
	stem := strings.TrimSuffix(fileName, ext)
	for n := 0; ; n++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if n > 0 {
			destinationPath = filepath.Join(destinationDir, fmt.Sprintf("%s (%d)%s", stem, n, ext))
		}
		publish := f.publish
		if publish == nil {
			publish = os.Link
		}
		err := publish(temp.Name(), destinationPath)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return fmt.Errorf("erro ao reservar destino: %w", err)
		}
		remove := f.removeSource
		if remove == nil {
			remove = removeUnchanged
		}
		if err := remove(sourcePath, info); err != nil {
			fmt.Printf("[PDF %q] Cópia publicada em %q, mas a origem não foi removida: %v\n", sourcePath, destinationPath, err)
			return fmt.Errorf("erro ao remover origem; cópia preservada em %q: %w", destinationPath, err)
		}
		fmt.Printf("[PDF %q] Movido com sucesso para %q.\n", sourcePath, destinationPath)
		return nil
	}
}

// completePDF é uma heurística: exige cabeçalho, EOF e estabilidade prévia.
func completePDF(file *os.File, size int64) bool {
	header := make([]byte, 5)
	if _, err := file.ReadAt(header, 0); err != nil || string(header) != "%PDF-" {
		return false
	}
	start := size - 1024
	if start < 0 {
		start = 0
	}
	tail := make([]byte, size-start)
	if _, err := file.ReadAt(tail, start); err != nil {
		return false
	}
	return bytes.HasSuffix(bytes.TrimSpace(tail), []byte("%%EOF"))
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func sameContent(ctx context.Context, path string, size int64, digest []byte) (bool, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() || info.Size() != size {
		return false, nil
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, &contextReader{ctx: ctx, reader: file}); err != nil {
		return false, err
	}
	return bytes.Equal(hash.Sum(nil), digest), nil
}
func removeUnchanged(path string, before os.FileInfo) error {
	now, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !os.SameFile(before, now) || before.Size() != now.Size() || !before.ModTime().Equal(now.ModTime()) {
		return errors.New("origem substituída ou alterada; arquivo preservado")
	}
	return os.Remove(path)
}
