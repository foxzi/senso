package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senso/internal/store"
)

// openIndexedDir создаёт временный каталог с одним файлом, индексирует его
// и возвращает путь каталога. Рабочий каталог на время теста меняется на него.
func openIndexedDir(t *testing.T, args ...string) string {
	t.Helper()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chdir(prev) })

	if err := RunIndex(append([]string{"--quiet"}, append(args, ".")...)); err != nil {
		t.Fatalf("RunIndex: %v", err)
	}
	return dir
}

func TestChunkParamsDiff(t *testing.T) {
	base := chunkParams{Chunker: "auto", ChunkSize: 1200, Overlap: 150}

	if diff := chunkParamsDiff(base, base); len(diff) != 0 {
		t.Errorf("equal params: diff = %v, want empty", diff)
	}

	other := chunkParams{Chunker: "text", ChunkSize: 800, Overlap: 150}
	diff := chunkParamsDiff(base, other)
	if len(diff) != 2 {
		t.Fatalf("diff = %v, want 2 entries", diff)
	}
	if !strings.Contains(diff[0], "--chunker") || !strings.Contains(diff[1], "--chunk-size") {
		t.Errorf("diff = %v, want chunker and chunk-size entries", diff)
	}
}

// TestIndexRecordsChunkParams проверяет, что параметры нарезки попадают в
// meta: без них последующая проверка совпадения невозможна.
func TestIndexRecordsChunkParams(t *testing.T) {
	dir := openIndexedDir(t, "--chunker", "text", "--chunk-size", "800", "--overlap", "50")

	s, err := store.OpenReadOnly(context.Background(), filepath.Join(dir, ".senso", "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	got, ok, err := loadChunkParams(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if !ok {
		t.Fatal("chunk params not recorded")
	}
	want := chunkParams{Chunker: "text", ChunkSize: 800, Overlap: 50}
	if got != want {
		t.Errorf("params = %+v, want %+v", got, want)
	}
}

// TestIndexRejectsOtherChunkParams проверяет, что повторная индексация с
// другими параметрами нарезки прекращается: иначе база смешала бы чанки
// двух стратегий.
func TestIndexRejectsOtherChunkParams(t *testing.T) {
	openIndexedDir(t)

	err := RunIndex([]string{"--quiet", "--chunker", "text", "."})
	if err == nil {
		t.Fatal("reindex with another chunker: error = nil, want error")
	}
	if !strings.Contains(err.Error(), "--chunker") {
		t.Errorf("error = %v, want mention of --chunker", err)
	}

	// Те же параметры по-прежнему принимаются.
	if err := RunIndex([]string{"--quiet", "."}); err != nil {
		t.Errorf("reindex with same params: %v", err)
	}
}

// TestIndexFillsMissingChunkParams проверяет, что базы, созданные версией
// senso без записи параметров, не отвергаются, а дополняются.
func TestIndexFillsMissingChunkParams(t *testing.T) {
	dir := openIndexedDir(t)
	dbPath := filepath.Join(dir, ".senso", "index.db")

	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetMeta(context.Background(), metaChunker, ""); err != nil {
		t.Fatal(err)
	}
	s.Close()

	if err := RunIndex([]string{"--quiet", "--chunker", "text", "."}); err != nil {
		t.Fatalf("reindex of a params-less database: %v", err)
	}

	ro, err := store.OpenReadOnly(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()

	got, ok, err := loadChunkParams(context.Background(), ro)
	if err != nil {
		t.Fatal(err)
	}
	if !ok || got.Chunker != "text" {
		t.Errorf("params = %+v (recorded=%v), want chunker text", got, ok)
	}
}

// TestCheckReportsChunkParamsMismatch проверяет, что check предупреждает о
// расхождении параметров нарезки до запуска index.
func TestCheckReportsChunkParamsMismatch(t *testing.T) {
	dir := openIndexedDir(t)

	if code := runCheckIn(t, dir, "--quiet"); code != 0 {
		t.Fatalf("same params: exit = %d, want 0", code)
	}
	if code := runCheckIn(t, dir, "--quiet", "--chunk-size", "800"); code != exitStale {
		t.Errorf("other chunk size: exit = %d, want %d", code, exitStale)
	}
}

// TestCheckUsesStoredChunkParams проверяет, что check без явных флагов
// нарезки берёт их из базы и не сообщает о ложном расхождении.
func TestCheckUsesStoredChunkParams(t *testing.T) {
	dir := openIndexedDir(t, "--chunker", "text", "--chunk-size", "800")

	if code := runCheckIn(t, dir, "--quiet"); code != 0 {
		t.Errorf("check without flags: exit = %d, want 0", code)
	}
	if code := runCheckIn(t, dir, "--quiet", "--chunker", "auto"); code != exitStale {
		t.Errorf("check with another chunker: exit = %d, want %d", code, exitStale)
	}
}

// TestCheckUsesStoredSelection проверяет, что правила отбора файлов тоже
// берутся из базы: иначе проиндексированные скрытые файлы выглядели бы
// исключёнными и индекс считался бы устаревшим.
func TestCheckUsesStoredSelection(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".hidden.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)

	if err := RunIndex([]string{"--quiet", "--hidden", "."}); err != nil {
		t.Fatalf("RunIndex: %v", err)
	}

	if code := runCheckIn(t, dir, "--quiet"); code != 0 {
		t.Errorf("check without flags: exit = %d, want 0", code)
	}
	if code := runCheckIn(t, dir, "--quiet", "--hidden=false"); code != exitStale {
		t.Errorf("check with --hidden=false: exit = %d, want %d", code, exitStale)
	}
}
