package main

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"
)

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

	t.Run("missing file", func(t *testing.T) {
		resetStorage()
		err := loadBudgetsFromFile(filepath.Join(t.TempDir(), "missing.json"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("loadBudgetsFromFile() error = %v, want %v", err, fs.ErrNotExist)
		}
	})
}
