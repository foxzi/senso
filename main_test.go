package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Ожидаемые коды завершения senso. Они - часть машинного интерфейса:
// скрипты и агенты полагаются на них, поэтому тесты ниже фиксируют их
// вместе с распределением вывода между stdout и stderr.
const (
	exitOK    = 0
	exitError = 1
	exitUsage = 2
	exitStale = 3
)

// captureOutput подменяет os.Stdout и os.Stderr пайпами на время вызова fn
// и возвращает то, что было в них напечатано.
func captureOutput(t *testing.T, fn func()) (stdout, stderr string) {
	t.Helper()

	origOut, origErr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	defer func() { os.Stdout, os.Stderr = origOut, origErr }()

	// Пайп имеет ограниченный буфер, поэтому читаем в фоне: иначе
	// команда с большим выводом заблокировалась бы на записи.
	outCh, errCh := make(chan string, 1), make(chan string, 1)
	go func() { data, _ := io.ReadAll(outR); outCh <- string(data) }()
	go func() { data, _ := io.ReadAll(errR); errCh <- string(data) }()

	fn()

	outW.Close()
	errW.Close()
	return <-outCh, <-errCh
}

// runQuiet выполняет команду senso, подавляя её вывод, и возвращает код
// завершения вместе с обоими потоками.
func runQuiet(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	stdout, stderr = captureOutput(t, func() { code = run(args) })
	return code, stdout, stderr
}

// mustIndexedTree создаёт дерево с одним файлом и индексирует его,
// возвращая путь к базе, к корню дерева и к файлу.
func mustIndexedTree(t *testing.T) (dbPath, root, filePath string) {
	t.Helper()
	root = t.TempDir()
	filePath = filepath.Join(root, "notes.txt")
	if err := os.WriteFile(filePath, []byte("index update notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dbPath = filepath.Join(t.TempDir(), "index.db")

	if code, _, stderr := runQuiet(t, "index", "--quiet", "--db", dbPath, root); code != exitOK {
		t.Fatalf("senso index вернул код %d: %s", code, stderr)
	}
	return dbPath, root, filePath
}

func TestExitCodeSuccess(t *testing.T) {
	dbPath, root, _ := mustIndexedTree(t)

	tests := []struct {
		name string
		args []string
	}{
		{"справка", []string{"help"}},
		{"версия", []string{"version"}},
		{"без аргументов", nil},
		{"поиск", []string{"search", "--db", dbPath, "notes"}},
		{"поиск без результатов", []string{"search", "--db", dbPath, "nothingmatchesthis"}},
		{"состояние", []string{"status", "--db", dbPath}},
		{"свежий индекс", []string{"check", "--quiet", "--db", dbPath, root}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if code, _, stderr := runQuiet(t, tt.args...); code != exitOK {
				t.Errorf("код завершения = %d, ожидался %d: %s", code, exitOK, stderr)
			}
		})
	}
}

func TestExitCodeUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"неизвестная команда", []string{"nosuchcommand"}},
		{"поиск без запроса", []string{"search"}},
		{"неизвестный формат", []string{"search", "--format", "yaml", "query"}},
		{"конфликт форматов", []string{"search", "--format", "json", "--paths-only", "query"}},
		{"неизвестный флаг", []string{"search", "--nosuchflag", "query"}},
		{"show без ссылки", []string{"show"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, stdout, stderr := runQuiet(t, tt.args...)
			if code != exitUsage {
				t.Errorf("код завершения = %d, ожидался %d", code, exitUsage)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, сообщения об ошибках должны идти в stderr", stdout)
			}
			if stderr == "" {
				t.Error("stderr пуст, ожидалось сообщение об ошибке")
			}
		})
	}
}

func TestExitCodeErrorWhenIndexMissing(t *testing.T) {
	missingDB := filepath.Join(t.TempDir(), "absent.db")

	code, stdout, stderr := runQuiet(t, "search", "--db", missingDB, "query")

	if code != exitError {
		t.Errorf("код завершения = %d, ожидался %d", code, exitError)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, ожидался пустой вывод", stdout)
	}
	if stderr == "" {
		t.Error("stderr пуст, ожидалось сообщение об ошибке")
	}
}

func TestExitCodeStaleIndex(t *testing.T) {
	dbPath, root, filePath := mustIndexedTree(t)
	if err := os.WriteFile(filePath, []byte("index update notes changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code, _, stderr := runQuiet(t, "check", "--quiet", "--db", dbPath, root); code != exitStale {
		t.Errorf("код завершения = %d, ожидался %d: %s", code, exitStale, stderr)
	}
}

// TestJSONV2GoesToStdoutErrorMessageToStderr фиксирует разделение потоков:
// машинный результат - в stdout, человекочитаемая диагностика - в stderr.
func TestJSONV2GoesToStdoutErrorMessageToStderr(t *testing.T) {
	dbPath, _, _ := mustIndexedTree(t)

	code, stdout, stderr := runQuiet(t, "search", "--db", dbPath, "--format", "json-v2", "notes")
	if code != exitOK {
		t.Fatalf("код завершения = %d, ожидался %d: %s", code, exitOK, stderr)
	}
	var resp map[string]any
	if err := json.Unmarshal([]byte(stdout), &resp); err != nil {
		t.Fatalf("stdout не разбирается как JSON: %v\n%s", err, stdout)
	}
	if resp["schema"] != float64(2) {
		t.Errorf("schema = %v, ожидалось 2", resp["schema"])
	}

	missingDB := filepath.Join(t.TempDir(), "absent.db")
	code, stdout, stderr = runQuiet(t, "search", "--db", missingDB, "--format", "json-v2", "notes")
	if code != exitError {
		t.Errorf("код завершения = %d, ожидался %d", code, exitError)
	}
	var errResp struct {
		Schema int `json:"schema"`
		Error  struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(stdout), &errResp); err != nil {
		t.Fatalf("stdout не содержит объект с ошибкой: %v\n%s", err, stdout)
	}
	if errResp.Error.Code != "no_index" {
		t.Errorf("код ошибки = %q, ожидался no_index", errResp.Error.Code)
	}
	if !strings.Contains(stderr, "senso search") {
		t.Errorf("stderr = %q, ожидалось человекочитаемое сообщение об ошибке", stderr)
	}
}
