package cli

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// indexIn запускает index в каталоге dir, как это сделал бы пользователь.
func indexIn(t *testing.T, dir string, args ...string) {
	t.Helper()

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)

	if err := RunIndex(append([]string{"--quiet"}, args...)); err != nil {
		t.Fatalf("RunIndex: %v", err)
	}
}

// indexedNames возвращает имена файлов каталога dir, оставшихся в индексе.
func indexedNames(t *testing.T, dir string) map[string]bool {
	t.Helper()
	s := openTestStore(t, filepath.Join(dir, ".senso", "index.db"))
	paths, err := s.ListPaths(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	names := make(map[string]bool, len(paths))
	for _, p := range paths {
		names[filepath.Base(p)] = true
	}
	return names
}

// Файл, который index больше не взял бы, должен уходить из индекса при
// переиндексации: иначе его старые чанки остаются в выдаче, а check вечно
// считает индекс устаревшим.
func TestIndexPrunesNoLongerIndexableFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"keep.txt", "emptied.txt", "binary.txt", "excluded.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("hello "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	indexIn(t, dir, ".")

	if err := os.WriteFile(filepath.Join(dir, "emptied.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "binary.txt"), binaryContent, 0o644); err != nil {
		t.Fatal(err)
	}
	indexIn(t, dir, "--exclude", "excluded.txt", ".")

	names := indexedNames(t, dir)
	if len(names) != 1 || !names["keep.txt"] {
		t.Errorf("в индексе остались %v, ожидался только keep.txt", names)
	}
	if code := runCheckIn(t, dir, "--quiet", "."); code != 0 {
		t.Errorf("check после переиндексации: код %d, ожидался 0", code)
	}
}

// Без --prune записи не подчищаются, как и раньше.
func TestIndexNoPruneKeepsExcludedFiles(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"keep.txt", "excluded.txt"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("hello "+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	indexIn(t, dir, ".")
	indexIn(t, dir, "--prune=false", "--exclude", "excluded.txt", ".")

	if names := indexedNames(t, dir); !names["excluded.txt"] {
		t.Errorf("excluded.txt удалён из индекса без --prune: %v", names)
	}
}

// При ошибке обхода список кандидатов неполон, поэтому файл нечитаемого
// каталога нельзя считать исключённым правилами и удалять.
func TestIndexPruneKeepsFilesOfUnreadableDir(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root читает каталоги без прав")
	}
	dir := t.TempDir()
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "inner.txt"), []byte("hello inner"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexIn(t, dir, ".")

	// Права только на вход: stat файла внутри работает, чтение каталога нет.
	if err := os.Chmod(sub, 0o111); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(sub, 0o755)
	indexIn(t, dir, ".")

	if names := indexedNames(t, dir); !names["inner.txt"] {
		t.Errorf("inner.txt удалён из индекса при нечитаемом каталоге: %v", names)
	}
}
