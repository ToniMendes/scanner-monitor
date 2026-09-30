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

	var pathchan = make(chan string, 10)
	var mkdirNameChan = make(chan string, 1)

	err := app.fs.GetPdf(pathchan)
	if err != nil {
		return err
	}

	err = app.ai.ReadPath(pathchan, mkdirNameChan)
	if err != nil {
		return err
	}

	err = app.fs.MakeDir(mkdirNameChan)
	if err != nil {
		return err
	}

	return nil
}
