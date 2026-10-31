package credential

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
	"golang.org/x/term"
)

const (
	service = "ScannerMonitor"
	user    = "OpenAI"
)

// KeyringStore persists the API key in the operating system credential store.
type KeyringStore struct{}

func (KeyringStore) Load() (string, error) {
	key, err := keyring.Get(service, user)
	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrAPIKeyNotFound
	}
	if err != nil {
		return "", fmt.Errorf("erro ao carregar credencial do keyring: %w", err)
	}
	return key, nil
}

func (KeyringStore) Save(key string) error {
	if strings.TrimSpace(key) == "" {
		return errors.New("API Key não pode estar vazia")
	}
	if err := keyring.Set(service, user, key); err != nil {
		return fmt.Errorf("erro ao salvar credencial no keyring: %w", err)
	}
	return nil
}

// TerminalPrompt reads a replacement key without echoing it to the terminal.
func TerminalPrompt(ctx context.Context) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	fmt.Print("Digite sua OpenAI API Key: ")
	key, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()
	if err != nil {
		return "", fmt.Errorf("erro ao ler API Key: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	value := strings.TrimSpace(string(key))
	if value == "" {
		return "", errors.New("API Key não pode estar vazia")
	}
	return value, nil
}
