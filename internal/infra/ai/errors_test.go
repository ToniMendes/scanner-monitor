package ai

import (
	"net/http"
	"testing"
	"time"

	"github.com/openai/openai-go/v3"
)

func TestClassifyAPIErrorByStatusAndCode(t *testing.T) {
	for _, tc := range []struct {
		name      string
		status    int
		code      string
		retryable bool
	}{
		{"unauthorized", http.StatusUnauthorized, "invalid_api_key", false},
		{"forbidden", http.StatusForbidden, "permission_denied", false},
		{"rate limit", http.StatusTooManyRequests, "rate_limit_exceeded", true},
		{"quota exhausted", http.StatusTooManyRequests, "insufficient_quota", false},
		{"server failure", http.StatusBadGateway, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := classifyAPIError(&openai.Error{StatusCode: tc.status, Code: tc.code})
			policy, ok := err.(retryPolicyError)
			if !ok || policy.Retryable() != tc.retryable {
				t.Fatalf("error=%T %v retryable=%v", err, err, ok && policy.Retryable())
			}
		})
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 10, 31, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		header http.Header
		want   time.Duration
	}{
		{"milliseconds", http.Header{"Retry-After-Ms": []string{"250"}}, 250 * time.Millisecond},
		{"seconds", http.Header{"Retry-After": []string{"2"}}, 2 * time.Second},
		{"invalid", http.Header{"Retry-After": []string{"nope"}}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseRetryAfter(tc.header, now); got != tc.want {
				t.Fatalf("got %s, want %s", got, tc.want)
			}
		})
	}
}
