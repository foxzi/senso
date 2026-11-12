package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// mustCreateEmptyDB создаёт в dir файл базы без схемы - такой остаётся
// после index --embed, не дошедшего до первого эмбеддинга.
func mustCreateEmptyDB(t *testing.T, dir string) string {
	t.Helper()
	dbPath := filepath.Join(dir, ".senso", "index.db")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatal(err)
	}
	openTestStore(t, dbPath)
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("файл базы не создан: %v", err)
	}
	return dbPath
}

// Читающие команды должны сообщать об отсутствии индекса, а не падать на
// отсутствующей таблице.
func TestOpenStoreWithoutSchemaReturnsIndexNotFound(t *testing.T) {
	dbPath := mustCreateEmptyDB(t, t.TempDir())

	cases := []struct {
		name string
		run  func([]string) error
		args []string
	}{
		{"search", RunSearch, []string{"--db", dbPath, "query"}},
		{"status", RunStatus, []string{"--db", dbPath}},
		{"show", RunShow, []string{"--db", dbPath, "/docs/spec.txt#0"}},
	}
	for _, c := range cases {
		err := c.run(c.args)
		if code := errorCode(err); code != errCodeNoIndex {
			t.Errorf("%s: код ошибки = %q (%v), ожидался %q", c.name, code, err, errCodeNoIndex)
		}
	}
}

func TestRunCheckWithoutSchemaReportsNoIndex(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustCreateEmptyDB(t, dir)

	if code := runCheckIn(t, dir, "--quiet", "."); code != 3 {
		t.Errorf("check с базой без схемы: код %d, ожидался 3", code)
	}
}
