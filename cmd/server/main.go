package main

import (
	"log"
	"scanner-monitor/internal/config"
	"scanner-monitor/internal/infra/ai"
)

func main() {
	err := config.StartViper()
	returnFatalError(err)

	openaiClient := ai.NewOpenAI()
	openaiClient.OpenAIClient()

	//aiRepo := ai.NewOpenAI()
	//fsRepo := filesystem.NewFileSystem()
	//
	//app := di.NewApp(fsRepo, aiRepo)
	//err = app.ToExecute()
	//returnFatalError(err)
}

func returnFatalError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
