package credential

import (
	"errors"
	"fmt"
	"log"
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

	if err != nil {
		apiKey, err = askAPIKey()
		if err != nil {
			return "", err
		}

		err = saveAPIKey(apiKey)
		if err != nil {
			log.Fatal(err)
		}
	}


	return apiKey, nil
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

	err := keyring.Set(service, user, apiKey)
	if err != nil {
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
