package domain

type FileExplorerRepository interface {
	GetPdf(channel chan string) error
}

type GeminiAIRepository interface {
	ReadPath(chan string) error
}