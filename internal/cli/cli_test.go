package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestShortenPath(t *testing.T) {
	cwd := "/home/user/project"

	cases := []struct {
		name string
		abs  string
		cwd  string
		want string
	}{
		{"внутри cwd", filepath.Join(cwd, "src", "main.go"), cwd, filepath.Join("src", "main.go")},
		{"равен cwd", cwd, cwd, "."},
		{"снаружи cwd", "/home/other/file.go", cwd, "/home/other/file.go"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := shortenPath(tc.abs, tc.cwd)
			if got != tc.want {
				t.Errorf("shortenPath(%q, %q) = %q, хотим %q", tc.abs, tc.cwd, got, tc.want)
			}
		})
	}
}

func TestSnippet(t *testing.T) {
	cases := []struct {
		name     string
		s        string
		maxRunes int
	}{
		{"короткая строка не меняется", "hello world", 100},
		{"переводы строк сохраняются", "hello\nworld\n\nfoo   bar", 100},
		{"кириллица обрезается по рунам", "привет мир, это длинный текст на кириллице для проверки", 10},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := snippetAround(tc.s, "", tc.maxRunes)
			if !utf8.ValidString(got) {
				t.Fatalf("snippet вернул невалидную UTF-8 строку: %q", got)
			}

			runeCount := utf8.RuneCountInString(got)
			maxAllowed := tc.maxRunes + utf8.RuneCountInString("...")
			if runeCount > maxAllowed {
				t.Errorf("snippetAround(%q, %d) вернул %d рун, ожидалось не более %d", tc.s, tc.maxRunes, runeCount, maxAllowed)
			}
		})
	}

	// короткая строка без изменений
	if got := snippetAround("hello world", "", 100); got != "hello world" {
		t.Errorf("snippetAround(short) = %q, хотим %q", got, "hello world")
	}

	// переводы строк и внутренние пробелы сохраняются как есть
	if got := snippetAround("hello\nworld\n\nfoo   bar", "", 100); got != "hello\nworld\n\nfoo   bar" {
		t.Errorf("snippetAround(multiline) = %q, хотим %q", got, "hello\nworld\n\nfoo   bar")
	}

	// обрезка по рунам не рвёт символы и добавляет многоточие
	got := snippetAround("привет мир", "", 3)
	want := "при..."
	if got != want {
		t.Errorf("snippetAround(cyrillic) = %q, хотим %q", got, want)
	}
}

// TestOpenStoreMissingDBFile проверяет, что читающая команда с несуществующим
// путём в --db получает понятную ошибку «индекс не найден» и, главное, не
// создаёт на этом пути пустой файл базы.
func TestOpenStoreMissingDBFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.db")

	s, _, err := openStore(context.Background(), path)
	if err == nil {
		s.Close()
		t.Fatal("openStore для несуществующей базы вернул успех")
	}
	if !strings.Contains(err.Error(), "index not found") && !strings.Contains(err.Error(), "индекс не найден") {
		t.Errorf("openStore вернул %v, ожидалась ошибка «индекс не найден»", err)
	}
	if _, statErr := os.Stat(path); statErr == nil {
		t.Errorf("openStore создал файл базы %s, хотя не должен был", path)
	}
}
