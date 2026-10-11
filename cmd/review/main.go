package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"scanner-monitor/internal/domain"
	"scanner-monitor/internal/infra/filesystem"
	"strings"
)

func approve(ctx context.Context, base, file, category string) error {
	model, err := domain.NewParametersPDF(category)
	if err != nil {
		return err
	}
	root, err := filepath.EvalSymlinks(filepath.Join(base, "revisao"))
	if err != nil {
		return err
	}
	source, err := filepath.EvalSymlinks(file)
	if err != nil {
		return err
	}
	root, err = filepath.Abs(root)
	if err != nil {
		return err
	}
	source, err = filepath.Abs(source)
	if err != nil {
		return err
	}
	relative, err := filepath.Rel(root, source)
	if err != nil || relative == "." || relative == ".." || filepath.IsAbs(relative) || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("arquivo precisa estar dentro da pasta de revisão")
	}
	if !strings.EqualFold(filepath.Ext(source), ".pdf") {
		return fmt.Errorf("somente PDFs podem ser aprovados")
	}
	fs := filesystem.NewFileSystem()
	dir, err := fs.CreateFolder(model.ClassificationName, base)
	if err != nil {
		return err
	}
	return fs.MovePDFContext(ctx, source, dir)
}
func main() {
	base := flag.String("base", "", "pasta base de saída")
	file := flag.String("file", "", "PDF dentro da pasta revisao")
	category := flag.String("category", "", "categoria aprovada")
	flag.Parse()
	if *base == "" || *file == "" || *category == "" {
		fmt.Fprintln(os.Stderr, "informe -base, -file e -category")
		os.Exit(1)
	}
	if err := approve(context.Background(), *base, *file, *category); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Println("Classificação aprovada e PDF organizado.")
}
