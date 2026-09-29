// Package di provides dependency injection wiring for the application.
package di

import (
	"scanner-monitor/internal/domain"
	"scanner-monitor/internal/infra/ai"
)

type App struct {
	fileExplorerRepository domain.FileExplorerRepository
}

func NewApp(repo domain.FileExplorerRepository) *App {
	return &App{
		fileExplorerRepository: repo,
	}
}

func (di *App) ToExecute() error {
	var channel = make(chan string, 10)

	err := di.fileExplorerRepository.GetPdf(channel)
	if err != nil {
		return err
	}

	g := ai.NewGeminiAI()
	g.ReadPath(channel)

	return nil
}
