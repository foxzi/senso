package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"senso/internal/store"
)

// withCapturedStderr запускает fn с os.Stderr, перенаправленным в пайп, и
// возвращает напечатанное в него.
func withCapturedStderr(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	defer func() { os.Stderr = orig }()

	fn()

	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// openTestStore открывает базу, созданную mustBuildShowIndex.
func openTestStore(t *testing.T, dbPath string) *store.Store {
	t.Helper()
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestStaleResultPathsFreshFile(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	s := openTestStore(t, dbPath)

	stale, err := staleResultPaths(s, []store.Result{{Path: filePath, Seq: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 0 {
		t.Fatalf("свежий файл не должен попадать в устаревшие, получено %v", stale)
	}
}

func TestStaleResultPathsModifiedFile(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	s := openTestStore(t, dbPath)

	if err := os.WriteFile(filePath, []byte("changed on disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	stale, err := staleResultPaths(s, []store.Result{{Path: filePath, Seq: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].reason != "modified" {
		t.Fatalf("ожидалось одно предупреждение modified, получено %v", stale)
	}
}

func TestStaleResultPathsMissingFile(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	s := openTestStore(t, dbPath)

	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}

	stale, err := staleResultPaths(s, []store.Result{{Path: filePath, Seq: 0}})
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 || stale[0].reason != "missing" {
		t.Fatalf("ожидалось одно предупреждение missing, получено %v", stale)
	}
}

// Несколько чанков одного файла в выдаче - обычный случай; предупреждение
// должно быть одно, иначе stderr забивается повторами.
func TestStaleResultPathsDeduplicatesPaths(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	s := openTestStore(t, dbPath)

	if err := os.WriteFile(filePath, []byte("changed on disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	results := []store.Result{
		{Path: filePath, Seq: 0},
		{Path: filePath, Seq: 1},
		{Path: filePath, Seq: 2},
	}
	stale, err := staleResultPaths(s, results)
	if err != nil {
		t.Fatal(err)
	}
	if len(stale) != 1 {
		t.Fatalf("ожидалось одно предупреждение на путь, получено %d", len(stale))
	}
}

func TestWarnStaleResultsWritesToStderr(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	s := openTestStore(t, dbPath)

	if err := os.WriteFile(filePath, []byte("changed on disk"), 0o644); err != nil {
		t.Fatal(err)
	}

	var runErr error
	out := withCapturedStderr(t, func() {
		runErr = warnStaleResults(s, []store.Result{{Path: filePath, Seq: 0}})
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if !strings.Contains(out, filePath) {
		t.Fatalf("в предупреждении нет пути файла: %q", out)
	}
	if strings.Count(strings.TrimSpace(out), "\n") != 0 {
		t.Fatalf("ожидалась одна строка предупреждения, получено %q", out)
	}
}

func TestWarnStaleResultsSilentForFreshFiles(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	s := openTestStore(t, dbPath)

	var runErr error
	out := withCapturedStderr(t, func() {
		runErr = warnStaleResults(s, []store.Result{{Path: filePath, Seq: 0}})
	})
	if runErr != nil {
		t.Fatal(runErr)
	}
	if out != "" {
		t.Fatalf("для свежего файла stderr должен быть пуст, получено %q", out)
	}
}
