package main

import (
	"strings"
	"testing"
)

func TestRunInvalidConfiguration(t *testing.T) {
	t.Chdir(t.TempDir())
	err := run()
	if err == nil || !strings.Contains(err.Error(), "não foi possível iniciar") {
		t.Fatalf("unexpected startup result: %v", err)
	}
}
