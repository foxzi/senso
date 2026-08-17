package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestReadIndexableEmptyFile проверяет код причины для пустого файла.
func TestReadIndexableEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.txt")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}

	content, skip, err := readIndexable(path, 0)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if skip != skipEmpty {
		t.Fatalf("skip = %q, ожидалось %q", skip, skipEmpty)
	}
	if content != nil {
		t.Fatalf("content = %q, ожидался nil", content)
	}
}

// TestReadIndexableTooLarge проверяет, что превышение лимита размера даёт
// код too_large, а не безымянный пропуск.
func TestReadIndexableTooLarge(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	if err := os.WriteFile(path, []byte("0123456789"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, skip, err := readIndexable(path, 5)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if skip != skipTooLarge {
		t.Fatalf("skip = %q, ожидалось %q", skip, skipTooLarge)
	}
}

// TestReadIndexableBinary проверяет код причины для бинарного содержимого.
func TestReadIndexableBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "blob.bin")
	if err := os.WriteFile(path, []byte{0x00, 0x01, 0x02, 0x00}, 0o600); err != nil {
		t.Fatal(err)
	}

	_, skip, err := readIndexable(path, 0)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if skip != skipBinary {
		t.Fatalf("skip = %q, ожидалось %q", skip, skipBinary)
	}
}

// TestReadIndexableText проверяет, что обычный текст не пропускается.
func TestReadIndexableText(t *testing.T) {
	path := filepath.Join(t.TempDir(), "a.txt")
	if err := os.WriteFile(path, []byte("привет"), 0o600); err != nil {
		t.Fatal(err)
	}

	content, skip, err := readIndexable(path, 0)
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if skip != "" {
		t.Fatalf("skip = %q, ожидалась пустая строка", skip)
	}
	if string(content) != "привет" {
		t.Fatalf("content = %q", content)
	}
}

// TestReadIndexableMissingFile проверяет, что ошибка чтения приходит с
// кодом read_failed и не теряется.
func TestReadIndexableMissingFile(t *testing.T) {
	_, _, err := readIndexable(filepath.Join(t.TempDir(), "nope.txt"), 0)
	if err == nil {
		t.Fatal("ожидалась ошибка чтения")
	}
	if code := failureCode(err); code != failRead {
		t.Fatalf("code = %q, ожидалось %q", code, failRead)
	}
}

// TestExtractIndexableBroken проверяет главную починку раздела: битый
// документ больше не исчезает из индекса молча, а даёт extract_failed.
func TestExtractIndexableBroken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.docx")
	data := []byte("это не zip-архив")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := readIndexable(path, 0)
	if err == nil {
		t.Fatal("ожидалась ошибка извлечения текста")
	}
	if code := failureCode(err); code != failExtract {
		t.Fatalf("code = %q, ожидалось %q", code, failExtract)
	}
}

// TestFailureCodeForeignError проверяет, что обычная ошибка не выдаёт себя
// за ошибку одного файла - такие ошибки должны валить всю команду.
func TestFailureCodeForeignError(t *testing.T) {
	if code := failureCode(errors.New("boom")); code != "" {
		t.Fatalf("code = %q, ожидалась пустая строка", code)
	}
}

// TestIndexReportCounters проверяет накопление пропусков по кодам.
func TestIndexReportCounters(t *testing.T) {
	r := newIndexReport()
	r.addSkip(skipBinary)
	r.addSkip(skipBinary)
	r.addSkip(skipEmpty)
	r.addFailure("/a/b.docx", failExtract, errors.New("bad zip"))

	if r.Skipped != 3 {
		t.Fatalf("Skipped = %d, ожидалось 3", r.Skipped)
	}
	if r.SkippedByCode[skipBinary] != 2 || r.SkippedByCode[skipEmpty] != 1 {
		t.Fatalf("SkippedByCode = %v", r.SkippedByCode)
	}
	if got := r.skipCodes(); len(got) != 2 || got[0] != skipBinary || got[1] != skipEmpty {
		t.Fatalf("skipCodes = %v, ожидался отсортированный список", got)
	}
	if len(r.Failed) != 1 || r.Failed[0].Code != failExtract || r.Failed[0].Message != "bad zip" {
		t.Fatalf("Failed = %v", r.Failed)
	}
}

// TestPrintIndexReportJSON проверяет форму машинного отчёта: одна строка
// JSON, а пустой список ошибок сериализуется как [], а не null.
func TestPrintIndexReportJSON(t *testing.T) {
	r := newIndexReport()
	r.Scanned = 2
	r.Indexed = 1
	r.Unchanged = 1

	var buf bytes.Buffer
	if err := printIndexReportJSON(&buf, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), `"failed":[]`) {
		t.Fatalf("ожидался пустой массив failed, получено: %s", buf.String())
	}

	var got map[string]any
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("отчёт не разбирается как JSON: %v", err)
	}
	for _, key := range []string{"scanned", "indexed", "updated", "unchanged", "deleted", "skipped", "failed", "interrupted"} {
		if _, ok := got[key]; !ok {
			t.Fatalf("в отчёте нет поля %q: %s", key, buf.String())
		}
	}
}

// TestReportExitErrorClean проверяет, что чистый прогон не даёт ошибки.
func TestReportExitErrorClean(t *testing.T) {
	if err := reportExitError(newIndexReport(), true); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

// TestReportExitErrorFailedWithoutStrict проверяет, что без --strict
// ошибки файлов не меняют код возврата.
func TestReportExitErrorFailedWithoutStrict(t *testing.T) {
	r := newIndexReport()
	r.addFailure("/a.docx", failExtract, errors.New("bad zip"))

	if err := reportExitError(r, false); err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
}

// TestReportExitErrorStrict проверяет ненулевой код в строгом режиме.
func TestReportExitErrorStrict(t *testing.T) {
	r := newIndexReport()
	r.addFailure("/a.docx", failExtract, errors.New("bad zip"))

	err := reportExitError(r, true)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("ожидался *ExitError, получено %v", err)
	}
	if exitErr.Code != 1 {
		t.Fatalf("Code = %d, ожидался 1", exitErr.Code)
	}
}

// TestReportExitErrorInterrupted проверяет код 130 при прерывании и его
// приоритет над ошибками файлов.
func TestReportExitErrorInterrupted(t *testing.T) {
	r := newIndexReport()
	r.Interrupted = true
	r.addFailure("/a.docx", failExtract, errors.New("bad zip"))

	err := reportExitError(r, true)
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("ожидался *ExitError, получено %v", err)
	}
	if exitErr.Code != 130 {
		t.Fatalf("Code = %d, ожидался 130", exitErr.Code)
	}
}

// TestPrintIndexSummarySkipsAndFailures проверяет, что человекочитаемая
// сводка показывает разбивку пропусков и список ошибок.
func TestPrintIndexSummarySkipsAndFailures(t *testing.T) {
	r := newIndexReport()
	r.Scanned = 4
	r.Indexed = 1
	r.addSkip(skipBinary)
	r.addSkip(skipEmpty)
	r.addFailure("/tmp/work/broken.docx", failExtract, errors.New("bad zip"))
	r.Database = "/tmp/work/.senso/index.db"

	var buf bytes.Buffer
	printIndexSummary(&buf, r, "/tmp/work")
	out := buf.String()

	for _, want := range []string{skipBinary, skipEmpty, failExtract, "broken.docx", ".senso/index.db"} {
		if !strings.Contains(out, want) {
			t.Fatalf("в сводке нет %q:\n%s", want, out)
		}
	}
}

// TestPrintIndexSummaryTruncatesFailures проверяет, что длинный список
// ошибок обрезается, а полный остаётся в машинном отчёте.
func TestPrintIndexSummaryTruncatesFailures(t *testing.T) {
	r := newIndexReport()
	for i := 0; i < maxFailuresShown+3; i++ {
		r.addFailure("/tmp/work/f.docx", failExtract, errors.New("bad zip"))
	}

	var buf bytes.Buffer
	printIndexSummary(&buf, r, "/tmp/work")

	if got := strings.Count(buf.String(), failExtract); got != maxFailuresShown {
		t.Fatalf("строк с ошибками = %d, ожидалось %d:\n%s", got, maxFailuresShown, buf.String())
	}
	if !strings.Contains(buf.String(), "3") {
		t.Fatalf("нет упоминания остатка:\n%s", buf.String())
	}
}
