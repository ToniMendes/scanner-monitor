// Package di provides dependency injection wiring for the application.
package di

import (
	"fmt"
	"scanner-monitor/internal/domain"

)

type App struct {
	ai domain.GeminiAIRepository
	fs domain.FileSystemRepository
}

func NewApp(fs domain.FileSystemRepository, ai domain.GeminiAIRepository) *App {
	return &App{
		ai:ai,
		fs: fs,
	}
}

func (app *App) ToExecute() error {

	fmt.Println("Executando app.")

	var channel = make(chan string, 10)

	err := app.fs.GetPdf(channel)
	if err != nil {
		return err
	}

	err = app.ai.ReadPath(channel)
	if err != nil {
		return err
	}

	return nil
}
