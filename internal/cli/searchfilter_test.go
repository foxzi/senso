package cli

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestNewResultFilterInactiveByDefault проверяет, что фильтр без флагов
// не активен и пропускает любой путь.
func TestNewResultFilterInactiveByDefault(t *testing.T) {
	f, err := newResultFilter("", "", "", "", nil)
	if err != nil {
		t.Fatalf("newResultFilter вернул ошибку: %v", err)
	}
	if f.Active() {
		t.Error("Active() = true, хотим false без флагов")
	}
	if !f.Match("/any/path.go") {
		t.Error("Match() = false, неактивный фильтр должен пропускать всё")
	}
}

// TestResultFilterPathGlob проверяет сопоставление --path с абсолютным
// путём и путём относительно корня, включая ** и одиночный *.
func TestResultFilterPathGlob(t *testing.T) {
	root := filepath.FromSlash("/home/user/project")
	cases := []struct {
		name string
		path string
		glob string
		want bool
	}{
		{"** покрывает поддиректорию", filepath.Join(root, "internal", "cli", "search.go"), "internal/**", true},
		{"** не покрывает корень сам по себе", filepath.Join(root, "main.go"), "internal/**", false},
		{"одиночная * по расширению", filepath.Join(root, "main.go"), "*.go", true},
		{"вложенный путь не совпадает с одиночной *", filepath.Join(root, "internal", "main.go"), "*.go", false},
		{"docs/**/*.md совпадает с вложенным файлом", filepath.Join(root, "docs", "ru", "usage.md"), "docs/**/*.md", true},
		{"абсолютный шаблон совпадает с абсолютным путём", root + "/internal/cli/search.go", root + "/internal/**", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := newResultFilter(tc.glob, "", "", "", []string{root})
			if err != nil {
				t.Fatalf("newResultFilter вернул ошибку: %v", err)
			}
			if got := f.Match(tc.path); got != tc.want {
				t.Errorf("Match(%q) с --path=%q = %v, хотим %v", tc.path, tc.glob, got, tc.want)
			}
		})
	}
}

// TestResultFilterExt проверяет фильтр --ext: список через запятую,
// точку можно указывать или не указывать, сравнение регистронезависимое.
func TestResultFilterExt(t *testing.T) {
	f, err := newResultFilter("", "go,.MD", "", "", nil)
	if err != nil {
		t.Fatalf("newResultFilter вернул ошибку: %v", err)
	}
	if !f.Active() {
		t.Fatal("Active() = false, хотим true при заданном --ext")
	}

	cases := []struct {
		path string
		want bool
	}{
		{"/p/main.go", true},
		{"/p/README.MD", true},
		{"/p/notes.md", true},
		{"/p/style.css", false},
	}
	for _, tc := range cases {
		if got := f.Match(tc.path); got != tc.want {
			t.Errorf("Match(%q) = %v, хотим %v", tc.path, got, tc.want)
		}
	}
}

// TestResultFilterExcludeTakesPriority проверяет, что --exclude
// отбрасывает путь, даже если он совпал с --path.
func TestResultFilterExcludeTakesPriority(t *testing.T) {
	root := filepath.FromSlash("/home/user/project")
	f, err := newResultFilter("internal/**", "", "internal/generated/**", "", []string{root})
	if err != nil {
		t.Fatalf("newResultFilter вернул ошибку: %v", err)
	}

	kept := filepath.Join(root, "internal", "cli", "search.go")
	if !f.Match(kept) {
		t.Errorf("Match(%q) = false, хотим true (совпадает с --path, не с --exclude)", kept)
	}

	dropped := filepath.Join(root, "internal", "generated", "code.go")
	if f.Match(dropped) {
		t.Errorf("Match(%q) = true, хотим false (--exclude должен победить --path)", dropped)
	}
}

// TestResultFilterRootRelativeAndAbsolute проверяет, что относительный
// и абсолютный варианты одного и того же пути дают одинаковый результат.
func TestResultFilterRootRelativeAndAbsolute(t *testing.T) {
	root := filepath.FromSlash("/home/user/project")
	f, err := newResultFilter("cmd/**", "", "", "", []string{root})
	if err != nil {
		t.Fatalf("newResultFilter вернул ошибку: %v", err)
	}

	path := filepath.Join(root, "cmd", "senso", "main.go")
	if !f.Match(path) {
		t.Errorf("Match(%q) = false, хотим true", path)
	}

	outside := filepath.Join(root, "internal", "main.go")
	if f.Match(outside) {
		t.Errorf("Match(%q) = true, хотим false", outside)
	}
}

// TestNewResultFilterRootFlag проверяет, что --root ограничивает
// результаты указанным корнем и требует, чтобы он входил в Roots().
func TestNewResultFilterRootFlag(t *testing.T) {
	rootA := filepath.FromSlash("/home/user/project-a")
	rootB := filepath.FromSlash("/home/user/project-b")
	roots := []string{rootA, rootB}

	f, err := newResultFilter("", "", "", rootA, roots)
	if err != nil {
		t.Fatalf("newResultFilter вернул ошибку: %v", err)
	}
	if !f.Active() {
		t.Fatal("Active() = false, хотим true при заданном --root")
	}

	inside := filepath.Join(rootA, "sub", "file.go")
	if !f.Match(inside) {
		t.Errorf("Match(%q) = false, хотим true (внутри --root)", inside)
	}

	outside := filepath.Join(rootB, "sub", "file.go")
	if f.Match(outside) {
		t.Errorf("Match(%q) = true, хотим false (другой корень)", outside)
	}

	if !f.Match(rootA) {
		t.Errorf("Match(%q) = false, хотим true (сам корень)", rootA)
	}
}

// TestNewResultFilterUnknownRoot проверяет, что --root с путём, которого
// нет среди Roots(), возвращает понятную usage-ошибку.
func TestNewResultFilterUnknownRoot(t *testing.T) {
	roots := []string{filepath.FromSlash("/home/user/project-a")}

	_, err := newResultFilter("", "", "", "/home/user/unknown", roots)
	if err == nil {
		t.Fatal("newResultFilter с неизвестным --root не вернул ошибку")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("ошибка не *UsageError: %T (%v)", err, err)
	}
}

// TestNewResultFilterRootNoIndexedRoots проверяет, что --root при пустом
// списке известных корней тоже даёт usage-ошибку, а не панику.
func TestNewResultFilterRootNoIndexedRoots(t *testing.T) {
	_, err := newResultFilter("", "", "", "/home/user/project", nil)
	if err == nil {
		t.Fatal("newResultFilter без известных корней не вернул ошибку")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("ошибка не *UsageError: %T (%v)", err, err)
	}
}

// TestResultFilterActive проверяет, что Active() реагирует на каждый
// из четырёх флагов независимо.
func TestResultFilterActive(t *testing.T) {
	root := filepath.FromSlash("/home/user/project")
	cases := []struct {
		name                         string
		path, ext, exclude, rootFlag string
	}{
		{"path", "*.go", "", "", ""},
		{"ext", "", "go", "", ""},
		{"exclude", "", "", "*.go", ""},
		{"root", "", "", "", root},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f, err := newResultFilter(tc.path, tc.ext, tc.exclude, tc.rootFlag, []string{root})
			if err != nil {
				t.Fatalf("newResultFilter вернул ошибку: %v", err)
			}
			if !f.Active() {
				t.Errorf("Active() = false, хотим true при заданном --%s", tc.name)
			}
		})
	}
}

// TestResultFilterNilIsInactive проверяет, что nil-фильтр (как если бы
// его не строили) ведёт себя как отсутствие фильтрации.
func TestResultFilterNilIsInactive(t *testing.T) {
	var f *resultFilter
	if f.Active() {
		t.Error("Active() на nil = true, хотим false")
	}
	if !f.Match("/any/path") {
		t.Error("Match() на nil = false, хотим true")
	}
}
