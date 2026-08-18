package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senso/internal/store"
)

// filePlaceholder подставляется вместо пути временного файла, чтобы
// golden-файл не зависел от каталога, в котором выполняется тест.
const filePlaceholder = "{FILE}"

// TestSearchJSONV2Golden фиксирует полную форму ответа json-v2: набор и
// порядок полей, а не только отдельные значения. Если ответ меняется
// осознанно, golden-файл надо обновить вместе с номером схемы или
// документацией - тест существует именно для того, чтобы такое изменение
// нельзя было внести незаметно.
func TestSearchJSONV2Golden(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	filter, err := newResultFilter("**/*.txt", "txt", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	opts := searchOptions{Query: "chunk text", K: 2, Snippet: 500, Hybrid: true, MaxPerFile: 2}
	results := []store.Result{
		{Path: filePath, Seq: 0, Text: "chunk text 0", Score: 0.032, StartLine: 1, EndLine: 1},
		{Path: filePath, Seq: 3, Text: "chunk text 3", Score: 0.016, StartLine: 4, EndLine: 4},
	}

	resp, err := buildSearchResponseV2(context.Background(), s, results, opts, filter)
	if err != nil {
		t.Fatalf("buildSearchResponseV2 вернул ошибку: %v", err)
	}
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	got := strings.ReplaceAll(string(data), filePath, filePlaceholder)
	assertGolden(t, "search_v2.golden", got)
}

func TestPrintSearchErrorJSONV2(t *testing.T) {
	out := withCapturedStdout(t, func() {
		if err := printSearchErrorJSONV2(withCode(errCodeNoVectors, errors.New("no vectors in the database"))); err != nil {
			t.Fatalf("printSearchErrorJSONV2 вернул ошибку: %v", err)
		}
	})

	var resp searchErrorResponseV2
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("вывод не разбирается как json-v2: %v\n%s", err, out)
	}
	if resp.Schema != searchSchemaV2 {
		t.Errorf("Schema = %d, ожидалось %d", resp.Schema, searchSchemaV2)
	}
	if resp.Error.Code != errCodeNoVectors {
		t.Errorf("код ошибки = %q, ожидался %q", resp.Error.Code, errCodeNoVectors)
	}
	if resp.Error.Message == "" {
		t.Error("сообщение об ошибке пустое")
	}
}

func TestRunSearchJSONV2PrintsErrorEnvelope(t *testing.T) {
	missingDB := filepath.Join(t.TempDir(), "absent.db")

	var runErr error
	out := withCapturedStdout(t, func() {
		runErr = RunSearch([]string{"--db", missingDB, "--format", formatJSONV2, "query"})
	})

	if runErr == nil {
		t.Fatal("RunSearch с отсутствующей базой не вернул ошибку")
	}

	var resp searchErrorResponseV2
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("stdout не содержит объект json-v2 с ошибкой: %v\n%s", err, out)
	}
	if resp.Error.Code != errCodeNoIndex {
		t.Errorf("код ошибки = %q, ожидался %q", resp.Error.Code, errCodeNoIndex)
	}
}

func TestRunSearchTextFormatDoesNotPrintErrorEnvelope(t *testing.T) {
	missingDB := filepath.Join(t.TempDir(), "absent.db")

	var runErr error
	out := withCapturedStdout(t, func() {
		runErr = RunSearch([]string{"--db", missingDB, "query"})
	})

	if runErr == nil {
		t.Fatal("RunSearch с отсутствующей базой не вернул ошибку")
	}
	if out != "" {
		t.Errorf("stdout = %q, в текстовом режиме ошибка должна уходить только в stderr", out)
	}
}

func TestErrorCode(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"без кода", errors.New("boom"), errCodeInternal},
		{"с кодом", withCode(errCodeEmbedFailed, errors.New("ollama unreachable")), errCodeEmbedFailed},
		{"ошибка аргументов", usagef("bad flag"), errCodeUsage},
		{"индекс не найден", errIndexNotFound(), errCodeNoIndex},
		{"обёрнутый код", withCode(errCodeNoVectors, errors.New("no vectors")), errCodeNoVectors},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errorCode(tt.err); got != tt.want {
				t.Errorf("errorCode() = %q, ожидалось %q", got, tt.want)
			}
		})
	}
}

func TestWithCodeKeepsMessageAndNil(t *testing.T) {
	if withCode(errCodeInternal, nil) != nil {
		t.Error("withCode(nil) вернул не nil")
	}
	err := withCode(errCodeNoVectors, errors.New("no vectors here"))
	if err.Error() != "no vectors here" {
		t.Errorf("сообщение = %q, ожидалось сохранение исходного текста", err.Error())
	}
}

// assertGolden сравнивает got с содержимым файла в testdata. Перевод строк
// в конце нормализуется, чтобы golden-файл можно было хранить с завершающим
// переводом строки.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("не удалось прочитать golden-файл %s: %v", path, err)
	}
	if strings.TrimRight(string(want), "\n") != strings.TrimRight(got, "\n") {
		t.Errorf("вывод не совпадает с %s\n--- получено ---\n%s\n--- ожидалось ---\n%s", path, got, want)
	}
}
