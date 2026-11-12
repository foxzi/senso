package cli

import (
	"errors"
	"testing"
)

// Опечатка в glob-шаблоне должна быть ошибкой аргументов, а не молча
// давать пустую выдачу или пустой отбор файлов.
func TestInvalidGlobIsUsageError(t *testing.T) {
	cases := []struct {
		name  string
		parse func() error
	}{
		{"search --path", func() error { _, err := parseSearchArgs([]string{"--path", "src/[a-", "query"}); return err }},
		{"search --exclude", func() error { _, err := parseSearchArgs([]string{"--exclude", "ok/**,[", "query"}); return err }},
		{"index --exclude", func() error { _, err := parseIndexArgs([]string{"--exclude", "[a-", "."}); return err }},
		{"index --include-hidden", func() error { _, err := parseIndexArgs([]string{"--include-hidden", "[a-", "."}); return err }},
		{"index --include-noisy", func() error { _, err := parseIndexArgs([]string{"--include-noisy", "[a-", "."}); return err }},
		{"index --noisy-patterns", func() error { _, err := parseIndexArgs([]string{"--noisy-patterns", "[a-", "."}); return err }},
		{"check --exclude", func() error { _, err := parseCheckArgs([]string{"--exclude", "[a-", "."}); return err }},
	}
	for _, c := range cases {
		var usageErr *UsageError
		if err := c.parse(); !errors.As(err, &usageErr) {
			t.Errorf("%s: ошибка = %v, ожидалась UsageError", c.name, err)
		}
	}
}

func TestValidGlobsAreAccepted(t *testing.T) {
	if _, err := parseSearchArgs([]string{"--path", "internal/**,*.go", "--exclude", "**/*_test.go", "query"}); err != nil {
		t.Errorf("search: %v", err)
	}
	if _, err := parseIndexArgs([]string{"--exclude", "vendor/**,*.png", "--include-hidden", ".github/**", "."}); err != nil {
		t.Errorf("index: %v", err)
	}
}
