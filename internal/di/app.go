// Package di provides dependency injection wiring for the application.
package di

import (
	"fmt"
	"scanner-monitor/internal/domain"
	"scanner-monitor/internal/infra/ai"
	"scanner-monitor/internal/infra/filesystem"

)

type repositories interface {
	domain.FileExplorerRepository
	domain.GeminiAIRepository
}
type AppRepository struct {
	repo repositories
}

func newAppRepository(r repositories) *AppRepository {
	return &AppRepository{
		repo: r,
	}
}

func NewApp() *AppRepository{
	fmt.Println("Iniciando configurações do app")

	aiRepo := ai.NewGeminiAI()
	fsRepo := filesystem.NewFileSystem()

	type hub struct {
		*ai.GeminiAI
		*filesystem.FileSystem
	}

	h := hub{
		aiRepo,
		fsRepo,
	}

	app := newAppRepository(h)

	return app
}

func (app *AppRepository) ToExecute() error {

	fmt.Println("Executando app.")

	var channel = make(chan string, 10)

	err := app.repo.GetPdf(channel)
	if err != nil {
		return err
	}

	err = app.repo.ReadPath(channel)
	if err != nil {
		return err
	}

	return nil
}
