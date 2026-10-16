package credential

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/zalando/go-keyring"
	"golang.org/x/term"
)

type Credential struct{}

func NewCredential() *Credential {
	return &Credential{}
}

const (
	service = "ScannerMonitor"
	user    = "OpenAI"
)

var ErrAPIKeyNotFound = errors.New("API Key não encontrada")

func (c *Credential) GetCredential() (string, error) {
	apiKey, err := loadAPIKey()

	if errors.Is(err, ErrAPIKeyNotFound) {
		apiKey, err = askAPIKey()
		if err != nil {
			return "", err
		}

		if err := saveAPIKey(apiKey); err != nil {
			return "", err
		}

		return apiKey, nil
	}

	if err != nil {
		return "", err
	}

	return apiKey, nil
}

func (c *Credential) ChangeCredential() (string, error) {
	fmt.Println("Informe a nova OpenAI API Key:")

	newAPIKey, err := askAPIKey()
	if err != nil {
		return "", err
	}

	if err := saveAPIKey(newAPIKey); err != nil {
		return "", err
	}

	fmt.Println("API Key atualizada com sucesso.")

	return newAPIKey, nil
}

func askAPIKey() (string, error) {
	fmt.Print("Digite sua OpenAI API Key: ")

	key, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Println()

	if err != nil {
		return "", fmt.Errorf("erro ao ler API Key: %w", err)
	}

	apiKey := strings.TrimSpace(string(key))

	if apiKey == "" {
		return "", errors.New("API Key não pode estar vazia")
	}

	return apiKey, nil
}

func saveAPIKey(apiKey string) error {
	if apiKey == "" {
		return errors.New("API Key não pode estar vazia")
	}

	if err := keyring.Set(service, user, apiKey); err != nil {
		return fmt.Errorf("erro ao salvar API Key: %w", err)
	}

	return nil
}

func loadAPIKey() (string, error) {
	apiKey, err := keyring.Get(service, user)

	if errors.Is(err, keyring.ErrNotFound) {
		return "", ErrAPIKeyNotFound
	}

	if err != nil {
		return "", fmt.Errorf("erro ao carregar API Key: %w", err)
	}

	return apiKey, nil
}
