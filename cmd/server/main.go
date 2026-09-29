package main

import (
	"log"
	"scanner-monitor/internal/config"
	"scanner-monitor/internal/di"

)

func main() {
	err := config.StartViper()
	returnFatalError(err)

	app := di.NewApp()
	err = app.ToExecute()
	returnFatalError(err)
}

func returnFatalError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
