package cli

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senso/internal/chunk"
	"senso/internal/store"
)

// --- parseRef ---

func TestParseRefValid(t *testing.T) {
	path, seq, err := parseRef("/docs/spec.docx#4")
	if err != nil {
		t.Fatalf("parseRef вернул ошибку: %v", err)
	}
	if path != "/docs/spec.docx" {
		t.Errorf("path = %q, ожидалось %q", path, "/docs/spec.docx")
	}
	if seq != 4 {
		t.Errorf("seq = %d, ожидалось 4", seq)
	}
}

func TestParseRefRelativePathBecomesAbsolute(t *testing.T) {
	path, _, err := parseRef("docs/spec.docx#0")
	if err != nil {
		t.Fatalf("parseRef вернул ошибку: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Errorf("path = %q, ожидался абсолютный путь", path)
	}
}

func TestParseRefMissingHash(t *testing.T) {
	if _, _, err := parseRef("/docs/spec.docx"); err == nil {
		t.Fatal("parseRef без '#' не вернул ошибку")
	}
}

func TestParseRefEmptyPath(t *testing.T) {
	if _, _, err := parseRef("#4"); err == nil {
		t.Fatal("parseRef с пустым путём не вернул ошибку")
	}
}

func TestParseRefEmptySeq(t *testing.T) {
	if _, _, err := parseRef("/docs/spec.docx#"); err == nil {
		t.Fatal("parseRef с пустым номером чанка не вернул ошибку")
	}
}

func TestParseRefNonNumericSeq(t *testing.T) {
	if _, _, err := parseRef("/docs/spec.docx#abc"); err == nil {
		t.Fatal("parseRef с нечисловым номером чанка не вернул ошибку")
	}
}

func TestParseRefNegativeSeq(t *testing.T) {
	if _, _, err := parseRef("/docs/spec.docx#-1"); err == nil {
		t.Fatal("parseRef с отрицательным номером чанка не вернул ошибку")
	}
}

// --- parseShowArgs ---

func TestParseShowArgsDefaults(t *testing.T) {
	opts, err := parseShowArgs([]string{"/docs/spec.docx#4"})
	if err != nil {
		t.Fatalf("parseShowArgs вернул ошибку: %v", err)
	}
	if opts.Path != "/docs/spec.docx" {
		t.Errorf("Path = %q, ожидалось %q", opts.Path, "/docs/spec.docx")
	}
	if opts.Seq != 4 {
		t.Errorf("Seq = %d, ожидалось 4", opts.Seq)
	}
	if opts.Before != 0 || opts.After != 0 {
		t.Errorf("Before=%d After=%d, ожидалось 0, 0", opts.Before, opts.After)
	}
	if opts.JSON {
		t.Error("JSON = true, ожидалось false")
	}
}

func TestParseShowArgsBeforeAfter(t *testing.T) {
	opts, err := parseShowArgs([]string{"--before", "1", "--after", "2", "/docs/spec.docx#4"})
	if err != nil {
		t.Fatalf("parseShowArgs вернул ошибку: %v", err)
	}
	if opts.Before != 1 || opts.After != 2 {
		t.Errorf("Before=%d After=%d, ожидалось 1, 2", opts.Before, opts.After)
	}
}

func TestParseShowArgsNegativeBefore(t *testing.T) {
	if _, err := parseShowArgs([]string{"--before", "-1", "/docs/spec.docx#4"}); err == nil {
		t.Fatal("parseShowArgs с --before -1 не вернул ошибку")
	}
}

func TestParseShowArgsNegativeAfter(t *testing.T) {
	if _, err := parseShowArgs([]string{"--after", "-1", "/docs/spec.docx#4"}); err == nil {
		t.Fatal("parseShowArgs с --after -1 не вернул ошибку")
	}
}

func TestParseShowArgsNoArgs(t *testing.T) {
	if _, err := parseShowArgs(nil); err == nil {
		t.Fatal("parseShowArgs(nil) не вернул ошибку")
	}
}

func TestParseShowArgsTooManyArgs(t *testing.T) {
	if _, err := parseShowArgs([]string{"/a#1", "/b#2"}); err == nil {
		t.Fatal("parseShowArgs с двумя аргументами не вернул ошибку")
	}
}

func TestParseShowArgsInvalidRef(t *testing.T) {
	if _, err := parseShowArgs([]string{"/docs/spec.docx"}); err == nil {
		t.Fatal("parseShowArgs с некорректной ссылкой не вернул ошибку")
	}
}

// --- hasSeq ---

func TestHasSeq(t *testing.T) {
	chunks := []store.Result{{Seq: 1}, {Seq: 3}, {Seq: 4}}
	if !hasSeq(chunks, 3) {
		t.Error("hasSeq(3) = false, ожидалось true")
	}
	if hasSeq(chunks, 2) {
		t.Error("hasSeq(2) = true, ожидалось false")
	}
	if hasSeq(nil, 0) {
		t.Error("hasSeq(nil, 0) = true, ожидалось false")
	}
}

// --- staleWarning ---

func TestStaleWarningMissing(t *testing.T) {
	msg := staleWarning("/docs/spec.docx", "missing")
	if !strings.Contains(msg, "/docs/spec.docx") {
		t.Errorf("staleWarning(missing) = %q, не содержит путь", msg)
	}
}

func TestStaleWarningModified(t *testing.T) {
	msg := staleWarning("/docs/spec.docx", "modified")
	if !strings.Contains(msg, "/docs/spec.docx") {
		t.Errorf("staleWarning(modified) = %q, не содержит путь", msg)
	}
}

// --- RunShow (сквозной тест на реальном сторе) ---

// mustBuildShowIndex создаёт базу senso во временном каталоге, кладёт в
// неё один файл с пятью чанками и возвращает путь к базе.
func mustBuildShowIndex(t *testing.T) (dbPath, filePath string) {
	t.Helper()
	dbPath = filepath.Join(t.TempDir(), "index.db")
	s, err := store.Open(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.Init("", 0, "/root"); err != nil {
		t.Fatal(err)
	}

	filePath = filepath.Join(t.TempDir(), "doc.txt")
	if err := os.WriteFile(filePath, []byte("stub"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filePath)
	if err != nil {
		t.Fatal(err)
	}

	chunks := make([]chunk.Chunk, 5)
	for i := range chunks {
		chunks[i] = chunk.Chunk{Text: "chunk text " + string(rune('0'+i)), StartLine: i + 1, EndLine: i + 1}
	}
	if err := s.ReplaceFile(filePath, info.ModTime().UnixNano(), info.Size(), "hash", chunks, nil); err != nil {
		t.Fatal(err)
	}
	return dbPath, filePath
}

// withCapturedStdout запускает fn с os.Stdout, перенаправленным в пайп, и
// возвращает то, что было в него напечатано.
func withCapturedStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	w.Close()
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestRunShowPrintsChunkText(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)

	out := withCapturedStdout(t, func() {
		if err := RunShow([]string{"--db", dbPath, filePath + "#2"}); err != nil {
			t.Fatalf("RunShow вернул ошибку: %v", err)
		}
	})

	if !strings.Contains(out, "chunk text 2") {
		t.Errorf("вывод RunShow = %q, не содержит текст запрошенного чанка", out)
	}
	if !strings.Contains(out, filePath+"#2") {
		t.Errorf("вывод RunShow = %q, не содержит ссылку %s#2", out, filePath)
	}
}

func TestRunShowWithBeforeAfterIncludesNeighbors(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)

	out := withCapturedStdout(t, func() {
		if err := RunShow([]string{"--db", dbPath, "--before", "1", "--after", "1", filePath + "#2"}); err != nil {
			t.Fatalf("RunShow вернул ошибку: %v", err)
		}
	})

	for _, want := range []string{"chunk text 1", "chunk text 2", "chunk text 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("вывод RunShow не содержит %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "chunk text 0") || strings.Contains(out, "chunk text 4") {
		t.Errorf("вывод RunShow содержит чанки за пределами окна --before/--after:\n%s", out)
	}
}

func TestRunShowJSON(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)

	out := withCapturedStdout(t, func() {
		if err := RunShow([]string{"--db", dbPath, "--json", filePath + "#2"}); err != nil {
			t.Fatalf("RunShow вернул ошибку: %v", err)
		}
	})

	if !strings.Contains(out, `"ref"`) || !strings.Contains(out, `"chunks"`) {
		t.Errorf("JSON-вывод RunShow не похож на ожидаемую структуру:\n%s", out)
	}
	if !strings.Contains(out, "chunk text 2") {
		t.Errorf("JSON-вывод RunShow не содержит текст чанка:\n%s", out)
	}
}

func TestRunShowUnknownChunkReturnsError(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)

	err := RunShow([]string{"--db", dbPath, filePath + "#99"})
	if err == nil {
		t.Fatal("RunShow с несуществующим номером чанка не вернул ошибку")
	}
}

func TestRunShowUnknownFileReturnsError(t *testing.T) {
	dbPath, _ := mustBuildShowIndex(t)

	err := RunShow([]string{"--db", dbPath, "/nowhere/missing.txt#0"})
	if err == nil {
		t.Fatal("RunShow с несуществующим файлом не вернул ошибку")
	}
}

func TestRunShowMissingDBReturnsIndexNotFoundError(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "missing.db")

	err := RunShow([]string{"--db", dbPath, "/docs/spec.docx#0"})
	if err == nil {
		t.Fatal("RunShow с несуществующей базой не вернул ошибку")
	}
}
