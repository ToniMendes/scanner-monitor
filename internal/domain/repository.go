package domain

type FileSystemRepository interface {
	GetPdf(channel chan string) error
}

type GeminiAIRepository interface {
	ReadPath(chan string) error
}