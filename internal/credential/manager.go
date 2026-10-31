package credential

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrAPIKeyNotFound    = errors.New("API Key não encontrada")
	ErrInvalidCredential = errors.New("credencial OpenAI inválida")
)

type Store interface {
	Load() (string, error)
	Save(string) error
}

type Prompt func(context.Context) (string, error)
type Validator func(context.Context, string) error

type Snapshot struct {
	Key        string
	Generation uint64
}

// Manager serializes validation and replacement of the API key. The key is
// published in memory only after validation and persistence both succeed.
type Manager struct {
	mu          sync.Mutex
	store       Store
	prompt      Prompt
	current     Snapshot
	initialized bool
}

func NewManager(store Store, prompt Prompt) *Manager {
	return &Manager{store: store, prompt: prompt}
}

func (m *Manager) Initialize(ctx context.Context, validate Validator) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.initialized {
		return m.current, nil
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if m.store == nil || m.prompt == nil || validate == nil {
		return Snapshot{}, errors.New("gerenciador de credenciais incompleto")
	}

	key, err := m.store.Load()
	switch {
	case err == nil:
		if strings.TrimSpace(key) == "" {
			err = ErrInvalidCredential
		} else {
			err = validate(ctx, key)
		}
		if err == nil {
			return m.publish(key), nil
		}
		if !errors.Is(err, ErrInvalidCredential) {
			return Snapshot{}, fmt.Errorf("falha ao validar credencial armazenada: %w", err)
		}
	case errors.Is(err, ErrAPIKeyNotFound):
	default:
		return Snapshot{}, err
	}

	key, err = m.prompt(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if strings.TrimSpace(key) == "" {
		return Snapshot{}, errors.New("API Key não pode estar vazia")
	}
	if err := validate(ctx, key); err != nil {
		return Snapshot{}, fmt.Errorf("a nova credencial não foi validada: %w", err)
	}
	if err := m.store.Save(key); err != nil {
		return Snapshot{}, err
	}
	return m.publish(key), nil
}

// Refresh replaces a key only when the caller's failed generation is still
// current. Concurrent workers that observed the same 401 share one prompt.
func (m *Manager) Refresh(ctx context.Context, failedGeneration uint64, validate Validator) (Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.initialized {
		return Snapshot{}, errors.New("credencial ainda não foi inicializada")
	}
	if failedGeneration != m.current.Generation {
		return m.current, nil
	}
	if err := ctx.Err(); err != nil {
		return Snapshot{}, err
	}
	if validate == nil || m.prompt == nil || m.store == nil {
		return Snapshot{}, errors.New("gerenciador de credenciais incompleto")
	}
	key, err := m.prompt(ctx)
	if err != nil {
		return Snapshot{}, err
	}
	if strings.TrimSpace(key) == "" {
		return Snapshot{}, errors.New("API Key não pode estar vazia")
	}
	if err := validate(ctx, key); err != nil {
		return Snapshot{}, fmt.Errorf("a nova credencial não foi validada: %w", err)
	}
	if err := m.store.Save(key); err != nil {
		return Snapshot{}, err
	}
	return m.publish(key), nil
}

func (m *Manager) publish(key string) Snapshot {
	generation := m.current.Generation + 1
	if generation == 0 {
		generation = 1
	}
	m.current = Snapshot{Key: key, Generation: generation}
	m.initialized = true
	return m.current
}
