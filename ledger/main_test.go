package main

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestRemaining(t *testing.T) {
	tests := []struct {
		limit, spent, want float64
	}{
		{limit: 5000, spent: 4750.5, want: 249.5},
		{limit: 5000, spent: 5000, want: 0},
		{limit: 50, spent: 90, want: 0}, // the limit was lowered below the spending
	}
	for _, tt := range tests {
		if got := remaining(tt.limit, tt.spent); got != tt.want {
			t.Errorf("remaining(%v, %v) = %v, want %v", tt.limit, tt.spent, got, tt.want)
		}
	}
}

func TestLoadBudgetsFromFile(t *testing.T) {
	t.Run("bundled budgets.json", func(t *testing.T) {
		resetStorage()
		if err := loadBudgetsFromFile("budgets.json"); err != nil {
			t.Fatalf("loadBudgetsFromFile() error = %v", err)
		}
		if len(ListBudgets()) == 0 {
			t.Error("no budgets loaded from budgets.json")
		}
	})

	t.Run("null instead of an array", func(t *testing.T) {
		resetStorage()
		path := filepath.Join(t.TempDir(), "budgets.json")
		if err := os.WriteFile(path, []byte("null"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := loadBudgetsFromFile(path); err == nil {
			t.Fatal("loadBudgetsFromFile() error = nil, want an error for null")
		}
	})

	t.Run("missing file", func(t *testing.T) {
		resetStorage()
		err := loadBudgetsFromFile(filepath.Join(t.TempDir(), "missing.json"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("loadBudgetsFromFile() error = %v, want %v", err, fs.ErrNotExist)
		}
	})
}
