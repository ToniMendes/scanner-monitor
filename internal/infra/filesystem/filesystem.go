// Package filesystem provides utilities for finding files in directories.
package filesystem

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type FileSystem struct{}

func NewFileSystem() *FileSystem {
	return &FileSystem{}
}

func (f *FileSystem) GetPdf() error {
	const filePath = `C:\Users\srden\Downloads\pdfss\`
	var pdfChan = make(chan string, 10)

	getPath := func() error {
		walkDirFunc := func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if !d.IsDir() && strings.HasSuffix(strings.ToLower(d.Name()), ".pdf") {
				pdfChan <- path
			}
			return nil
		}

		err := filepath.WalkDir(filePath, walkDirFunc)
		if err != nil {
			return err
		}

		return nil
	}
	if err := getPath(); err != nil {
		fmt.Errorf("nao foi possivel localizar nenhum caminho")
		return err
	}

	readPath := func() error {
		return nil
	}

	_ = readPath
	return nil
}

func (f *FileSystem) MakeDir(mkdirNameChan <-chan string) error {
	fmt.Println("Criando Pasta.")
	// Caminho da pasta
	dirPath := fmt.Sprintf(`C:\Users\srden\Downloads\pdfss\%s\`, <-mkdirNameChan)

	// 0755 define as permissões (no Windows, o SO aplica a ACL padrão do usuário)
	err := os.Mkdir(dirPath, 0775)
	if err != nil {
		if os.IsExist(err) {
			fmt.Println("Pasta já existe.")
			return nil
		}
		return err
	}

	fmt.Println("Pasta criada com sucesso!")
	return nil
}
