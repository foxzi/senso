package cli

import (
	"path/filepath"
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
		{"переводы строк схлопнуты", "hello\nworld\n\nfoo   bar", 100},
		{"кириллица обрезается по рунам", "привет мир, это длинный текст на кириллице для проверки", 10},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := snippet(tc.s, tc.maxRunes)
			if !utf8.ValidString(got) {
				t.Fatalf("snippet вернул невалидную UTF-8 строку: %q", got)
			}

			runeCount := utf8.RuneCountInString(got)
			maxAllowed := tc.maxRunes + utf8.RuneCountInString("...")
			if runeCount > maxAllowed {
				t.Errorf("snippet(%q, %d) вернул %d рун, ожидалось не более %d", tc.s, tc.maxRunes, runeCount, maxAllowed)
			}
		})
	}

	// короткая строка без изменений, кроме схлопывания пробелов
	if got := snippet("hello world", 100); got != "hello world" {
		t.Errorf("snippet(short) = %q, хотим %q", got, "hello world")
	}

	// переводы строк и повторяющиеся пробелы схлопнуты в один пробел
	if got := snippet("hello\nworld\n\nfoo   bar", 100); got != "hello world foo bar" {
		t.Errorf("snippet(multiline) = %q, хотим %q", got, "hello world foo bar")
	}

	// обрезка по рунам не рвёт символы и добавляет многоточие
	got := snippet("привет мир", 3)
	want := "при..."
	if got != want {
		t.Errorf("snippet(cyrillic) = %q, хотим %q", got, want)
	}
}
