package domain

type ExplorerRepository interface {
	GetPdf(channel chan string) error
}