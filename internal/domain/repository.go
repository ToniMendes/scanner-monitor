package domain

type FileExplorerRepository interface {
	GetPdf(channel chan string) error
}