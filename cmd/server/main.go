package main

import (
	"log"
	"scanner-monitor/internal/config"
	"scanner-monitor/internal/di"
	"scanner-monitor/internal/infra/ai"
	"scanner-monitor/internal/infra/filesystem"

)

func main() {
	err := config.StartViper()
	returnFatalError(err)

	aiRepo := ai.NewGeminiAI()
	fsRepo := filesystem.NewFileSystem()

	app := di.NewApp(fsRepo, aiRepo)
	err = app.ToExecute()
	returnFatalError(err)
}

func returnFatalError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
