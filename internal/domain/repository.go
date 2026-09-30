package domain

type FileSystemRepository interface {
	GetPdf(pathChan chan<- string) error
	MakeDir(mkdirChan <-chan string) error
}

type GeminiAIRepository interface {
	ReadPath(<-chan string, chan<- string) error
}