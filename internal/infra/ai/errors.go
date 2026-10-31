package ai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
)

type retryPolicyError interface {
	Retryable() bool
	RetryAfter() time.Duration
}

type apiFailure struct {
	cause      error
	statusCode int
	code       string
	retryAfter time.Duration
	retryable  bool
}

func (e *apiFailure) Error() string {
	if e.code != "" {
		return fmt.Sprintf("OpenAI API error: %d (%s)", e.statusCode, e.code)
	}
	return fmt.Sprintf("OpenAI API error: %d", e.statusCode)
}
func (e *apiFailure) Unwrap() error             { return e.cause }
func (e *apiFailure) Retryable() bool           { return e.retryable }
func (e *apiFailure) RetryAfter() time.Duration { return e.retryAfter }

func isUnauthorized(err error) bool {
	var apiErr *openai.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusUnauthorized
}

func classifyAPIError(err error) error {
	var apiErr *openai.Error
	if !errors.As(err, &apiErr) {
		return err
	}
	status := apiErr.StatusCode
	code := strings.ToLower(apiErr.Code + " " + apiErr.Type)
	retryable := status == http.StatusRequestTimeout || status >= 500
	if status == http.StatusTooManyRequests {
		retryable = !strings.Contains(code, "insufficient_quota") &&
			!strings.Contains(code, "billing_hard_limit")
	}
	var retryAfter time.Duration
	if apiErr.Response != nil {
		retryAfter = parseRetryAfter(apiErr.Response.Header, time.Now())
	}
	codeLabel := apiErr.Code
	if codeLabel == "" {
		codeLabel = apiErr.Type
	}
	return &apiFailure{cause: err, statusCode: status, code: codeLabel, retryAfter: retryAfter, retryable: retryable}
}

func parseRetryAfter(header http.Header, now time.Time) time.Duration {
	if value := strings.TrimSpace(header.Get("Retry-After-Ms")); value != "" {
		if millis, err := strconv.ParseFloat(value, 64); err == nil && millis >= 0 {
			return time.Duration(millis * float64(time.Millisecond))
		}
	}
	value := strings.TrimSpace(header.Get("Retry-After"))
	if value == "" {
		return 0
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds >= 0 {
		return time.Duration(seconds * float64(time.Second))
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}

func (open *OpenAI) authenticatedCall(ctx context.Context, call func(*openai.Client) error) error {
	state := open.current.Load()
	if state == nil || state.client == nil {
		return errors.New("cliente OpenAI não inicializado")
	}
	err := call(state.client)
	if err == nil {
		return nil
	}
	if !isUnauthorized(err) || open.credentials == nil {
		return classifyAPIError(err)
	}

	snapshot, refreshErr := open.credentials.Refresh(ctx, state.generation, open.validateKey)
	if refreshErr != nil {
		return fmt.Errorf("não foi possível renovar a credencial OpenAI: %w", refreshErr)
	}
	updated := &clientState{client: open.newClient(snapshot.Key), generation: snapshot.Generation}
	for {
		current := open.current.Load()
		if current != nil && current.generation > updated.generation {
			updated = current
			break
		}
		if open.current.CompareAndSwap(current, updated) {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return classifyAPIError(call(updated.client))
}

var _ retryPolicyError = (*apiFailure)(nil)
