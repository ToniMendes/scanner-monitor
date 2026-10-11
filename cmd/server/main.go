package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"scanner-monitor/internal/config"
	"scanner-monitor/internal/infra/ai"
	"scanner-monitor/internal/infra/filesystem"
	"scanner-monitor/internal/worker"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	err := config.StartViper()
	returnFatalError(err)

	aiRepo, errAi := ai.NewOpenAI(config.Env.ModelOpenAI, config.Env.OpenaiSDKKey)
	returnFatalError(errAi)

	fsRepo := filesystem.NewFileSystem()

	w := worker.NewMonitor(aiRepo, fsRepo)

	errChan := w.Monitoring(ctx, config.Env.FilePathBase, config.Env.DirPathBase)

	for err := range errChan {
		returnFatalError(err)
	}
}

func returnFatalError(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
