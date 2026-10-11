package worker

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"scanner-monitor/internal/domain"
	"sync"
	"time"
)

type Monitor struct {
	ai             domain.GeminiAIRepository
	fs             domain.FileSystemRepository
	RetryDelay     time.Duration
	MaxAttempts    int
	ReviewRequired bool
}

func NewMonitor(aiRepo domain.GeminiAIRepository, fsRepo domain.FileSystemRepository) *Monitor {
	return &Monitor{
		ai:          aiRepo,
		fs:          fsRepo,
		RetryDelay:  time.Second,
		MaxAttempts: 3,
	}
}

func (m *Monitor) Monitoring(ctx context.Context, filePathBase, dirPathBase string) chan error {
	ctx, cancel := context.WithCancel(ctx)
	worker := 20
	var wg sync.WaitGroup
	var fileChan = make(chan *os.File)

	var errChan = make(chan error, 1)
	report := func(err error) {
		if err != nil && ctx.Err() == nil {
			select {
			case errChan <- err:
			case <-ctx.Done():
			}
		}
	}
	var active sync.Map

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(fileChan)
		for ctx.Err() == nil {
			err := m.fs.GetPdf(ctx, fileChan, filePathBase)
			if err == nil {
				return
			}
			report(fmt.Errorf("monitoramento da entrada: %w", err))
			if wait(ctx, m.RetryDelay) != nil {
				return
			}
		}
	}()

	for i := 0; i < worker; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			for file := range fileChan {
				if file == nil {
					report(errors.New("arquivo PDF não informado"))
					continue
				}
				if ctx.Err() != nil {
					file.Close()
					continue
				}
				name := file.Name()
				if _, loaded := active.LoadOrStore(name, true); loaded {
					file.Close()
					continue
				}
				report(m.process(ctx, file, dirPathBase))
				active.Delete(name)
			}

		}(i)
	}

	go func() {
		wg.Wait()
		cancel()
		close(errChan)
	}()

	return errChan
}

func wait(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		delay = time.Millisecond
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// retry limita cada tentativa e mantém o erro final associado à etapa.
func (m *Monitor) retry(ctx context.Context, stage string, fn func(context.Context) error) error {
	attempts := m.MaxAttempts
	if attempts <= 0 {
		attempts = 3
	}
	var err error
	for i := 0; i < attempts; i++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		attemptCtx, cancel := context.WithTimeout(ctx, time.Minute)
		err = fn(attemptCtx)
		cancel()
		if err == nil {
			return nil
		}
		if i+1 < attempts {
			if waitErr := wait(ctx, m.RetryDelay*time.Duration(1<<i)); waitErr != nil {
				return waitErr
			}
		}
	}
	return fmt.Errorf("%s falhou após %d tentativas: %w", stage, attempts, err)
}

func (m *Monitor) process(ctx context.Context, file *os.File, base string) error {
	name := file.Name()
	file.Close()
	var model *domain.ParametersPDF
	if err := m.retry(ctx, "classificação de "+name, func(attempt context.Context) error {
		input, err := os.Open(name)
		if err != nil {
			return err
		}
		defer input.Close()
		before, err := input.Stat()
		if err != nil {
			return err
		}
		model, err = m.ai.AnalyzePDF(attempt, input)
		if err == nil {
			after, statErr := os.Stat(name)
			if statErr != nil {
				return statErr
			}
			if !os.SameFile(before, after) || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
				return errors.New("PDF alterado durante a classificação")
			}
		}
		if err == nil && model == nil {
			return errors.New("classificação não informada")
		}
		if err == nil {
			model, err = domain.NewParametersPDF(model.ClassificationName)
		}
		return err
	}); err != nil {
		return err
	}
	if m.ReviewRequired {
		base = filepath.Join(base, "revisao")
	}
	var dir string
	if err := m.retry(ctx, "criação de pasta para "+name, func(context.Context) error {
		var err error
		dir, err = m.fs.CreateFolder(model.ClassificationName, base)
		return err
	}); err != nil {
		return err
	}
	return m.retry(ctx, "movimentação de "+name, func(attempt context.Context) error {
		if fs, ok := m.fs.(interface {
			MovePDFContext(context.Context, string, string) error
		}); ok {
			return fs.MovePDFContext(attempt, name, dir)
		}
		return m.fs.MovePDF(name, dir)
	})
}
