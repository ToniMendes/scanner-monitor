package worker

import (
	"context"
	"errors"
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

func (m *Monitor) Monitoring(ctx context.Context, filePathBase, dirPathBase string) chan error {
	ctx, cancel := context.WithCancel(ctx)
	worker := 20
	var wg sync.WaitGroup
	var fileChan = make(chan *os.File)

	var errChan = make(chan error, 1)
	var once sync.Once
	report := func(err error) {
		if err != nil && !errors.Is(err, context.Canceled) {
			once.Do(func() { errChan <- err; cancel() })
		}
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		defer close(fileChan)
		err := m.fs.GetPdf(ctx, fileChan, filePathBase)
		report(err)
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

				model, err := m.ai.AnalyzePDF(ctx, file)
				file.Close()
				if err != nil {
					report(err)
					continue
				}
				if model == nil {
					report(errors.New("classificação não informada"))
					continue
				}

				dirPath, err := m.fs.CreateFolder(model.ClassificationName, dirPathBase)
				if err != nil {
					report(err)
					continue
				}

				report(m.fs.MovePDF(name, dirPath))
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
