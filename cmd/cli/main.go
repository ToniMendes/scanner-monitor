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
	if err != nil {
		log.Printf("não foi possível iniciar: %v", err)
		return
	}

	aiRepo, errAi := ai.NewOpenAI(config.Env.ModelOpenAI, config.Env.OpenaiSDKKey)
	if errAi != nil {
		log.Printf("não foi possível iniciar a IA: %v", errAi)
		return
	}

	fsRepo := filesystem.NewFileSystem()

	w := worker.NewMonitor(aiRepo, fsRepo)

	errChan := w.Monitoring(ctx, config.Env.FilePathBase, config.Env.DirPathBase)

	for err := range errChan {
		log.Printf("falha no processamento (monitor continua ativo): %v", err)
	}
}
