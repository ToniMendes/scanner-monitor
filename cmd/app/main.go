package main

import (
	"log"
	"scanner-monitor/internal/configs"
	"scanner-monitor/internal/infra/gemini"

)

func main() {
	err := configs.StartViper()
	returnFatalError(err)

	ai := gemini.NewGeminiIA()
	err = ai.InitGemini()
	returnFatalError(err)
}

func returnFatalError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
