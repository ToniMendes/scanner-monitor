package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestApprove(t *testing.T) {
	for _, scenario := range []string{"approve", "correct category", "outside review", "invalid category", "missing file", "not pdf"} {
		t.Run(scenario, func(t *testing.T) {
			base := t.TempDir()
			review := filepath.Join(base, "revisao", "outro")
			if err := os.MkdirAll(review, 0700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(review, "scan.pdf")
			category := "outro"
			if scenario == "correct category" {
				category = "atestado"
			}
			if scenario == "invalid category" {
				category = "../escape"
			}
			if scenario == "outside review" {
				path = filepath.Join(base, "outside.pdf")
			}
			if scenario == "not pdf" {
				path = filepath.Join(review, "scan.txt")
			}
			if err := os.WriteFile(path, []byte("pdf"), 0600); err != nil {
				t.Fatal(err)
			}
			if scenario == "missing file" {
				path = filepath.Join(review, "missing.pdf")
			}
			err := approve(context.Background(), base, path, category)
			success := scenario == "approve" || scenario == "correct category"
			if (err == nil) != success {
				t.Fatalf("%s: %v", scenario, err)
			}
			if success {
				if _, err := os.Stat(filepath.Join(base, category, "scan.pdf")); err != nil {
					t.Fatal(err)
				}
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("source remains: %v", err)
				}
			}
		})
	}
}
