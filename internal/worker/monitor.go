package worker

import (
	"context"
	"os"
	"scanner-monitor/internal/domain"
	"sync"
)

type Monitor struct {
	ai domain.GeminiAIRepository
	fs domain.FileSystemRepository
}

func NewMonitor(aiRepo domain.GeminiAIRepository, fsRepo domain.FileSystemRepository) *Monitor {
	return &Monitor{
		ai: aiRepo,
		fs: fsRepo,
	}
}

func (m *Monitor) Monitoring(ctx context.Context) chan error {
	worker := 5
	var wg sync.WaitGroup
	var fileChan = make(chan *os.File)

	var errChan = make(chan error, 1)

	go func() {
		err := m.fs.GetPdf(ctx, fileChan)
		if err != nil {
			errChan <- err
			return
		}
	}()

	for i := 0; i < worker; i++ {
		wg.Add(1)

		go func(i int) {
			defer wg.Done()

			file := <-fileChan

			model, err := m.ai.AnalyzePDF(ctx, file)
			if err != nil {
				errChan <- err
				return
			}

			dirPath, err := m.fs.CreateFolder(model.ClassificationName)
			if err != nil {
				errChan <- err
				return
			}

			err = m.fs.MovePDF(file.Name(), dirPath)
			if err != nil {
				errChan <- err
				return
			}

		}(i)
	}

	go func() {
		wg.Wait()
	}()

	return errChan
}
