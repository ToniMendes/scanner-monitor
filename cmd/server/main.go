package main

import (
	"log"
	"scanner-monitor/internal/config"
	"scanner-monitor/internal/di"
	"scanner-monitor/internal/infra/filesystem"

)

func main() {
	err := config.StartViper()
	returnFatalError(err)

	fsRepo := filesystem.NewFileExplorer()
	app := di.NewApp(fsRepo)

	err = app.ToExecute()
	returnFatalError(err)
}

func returnFatalError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
