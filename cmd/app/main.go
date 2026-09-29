package main

import (
	"log"
	"scanner-monitor/internal/configs"
	"scanner-monitor/internal/infra/ai"
	"scanner-monitor/internal/infra/fileexplorer"
)

func main() {
	err := configs.StartViper()
	returnFatalError(err)

	var channel = make(chan string, 10)

	repo := fileexplorer.NewFileExplorer()
	err = repo.GetPdf(channel)

	g := ai.NewGeminiAI()

	g.ReadPath(channel)

	returnFatalError(err)
}

func returnFatalError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
