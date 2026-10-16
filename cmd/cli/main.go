package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"scanner-monitor/internal/config"
	"scanner-monitor/internal/credential"
	"scanner-monitor/internal/infra/ai"
	"scanner-monitor/internal/infra/filesystem"
	"scanner-monitor/internal/worker"
	"syscall"
)

func main() {
	if err := run(); err != nil {
		log.Print(err)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(
		context.Background(),
		os.Interrupt,
		syscall.SIGTERM,
	)
	defer stop()

	err := config.StartViper()
	if err != nil {
		return fmt.Errorf("não foi possível iniciar: %w", err)
	}

	cr := credential.NewCredential()
	apiKey, errGetCredential := cr.GetCredential()

	aiRepo, errAi := ai.NewOpenAI(config.Env.ModelOpenAI, apiKey)
	if errAi != nil {
		return fmt.Errorf("não foi possível iniciar a IA: %w", errAi)
	}

	if errGetCredential != nil {
		return errGetCredential
	}

	fsRepo := filesystem.NewFileSystem()

	w := worker.NewMonitor(aiRepo, fsRepo)

	errChan := w.Monitoring(ctx, config.Env.FilePathBase, config.Env.DirPathBase)

	for err := range errChan {
		log.Printf("falha no processamento (monitor continua ativo): %v", err)
	}
	return nil
}
