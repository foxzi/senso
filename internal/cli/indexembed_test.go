package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeOllama поднимает сервер эмбеддингов: на каждый текст отвечает
// вектором из трёх чисел. Пока down установлен, отвечает ошибкой запроса
// (без повторов со стороны клиента).
func fakeOllama(t *testing.T, down *bool) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" || *down {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		embeddings := make([][]float32, len(req.Input))
		for i := range embeddings {
			embeddings[i] = []float32{1, 0.5, 0.25}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": req.Model, "embeddings": embeddings})
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

// mustChdirTree создаёт каталог с одним текстовым файлом и переходит в него
// до конца теста.
func mustChdirTree(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("alpha beta gamma"), 0o644); err != nil {
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
	return dir
}

func storedModel(t *testing.T, dir string) (string, int) {
	t.Helper()
	s := openTestStore(t, filepath.Join(dir, ".senso", "index.db"))
	model, dim, err := s.Meta(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return model, dim
}

// Эмбеддинги, добавленные к лексическому индексу, должны записать модель:
// без неё гибридный поиск не знает, чем строить эмбеддинг запроса.
func TestIndexEmbedAfterLexicalRecordsModel(t *testing.T) {
	down := false
	url := fakeOllama(t, &down)
	dir := mustChdirTree(t)

	if err := RunIndex([]string{"--quiet", "."}); err != nil {
		t.Fatalf("лексический index: %v", err)
	}
	if err := RunIndex([]string{"--quiet", "--embed", "--ollama", url, "--model", "test-model", "."}); err != nil {
		t.Fatalf("index --embed: %v", err)
	}

	if model, dim := storedModel(t, dir); model != "test-model" || dim != 3 {
		t.Errorf("в базе модель %q, dim %d; ожидались test-model, 3", model, dim)
	}
	var searchErr error
	out := withCapturedStdout(t, func() {
		searchErr = RunSearch([]string{"--hybrid", "--ollama", url, "--paths-only", "alpha"})
	})
	if searchErr != nil || !strings.Contains(out, "a.txt") {
		t.Errorf("search --hybrid: ошибка %v, вывод %q", searchErr, out)
	}

	err := RunIndex([]string{"--quiet", "--embed", "--ollama", url, "--model", "other-model", "."})
	if err == nil {
		t.Error("index --embed с другой моделью прошёл, ожидалась ошибка")
	}
}

// Первый запуск с --embed при недоступном сервисе эмбеддингов должен
// оставить пустой рабочий индекс, а не файл базы без схемы.
func TestIndexEmbedFailureLeavesUsableIndex(t *testing.T) {
	down := true
	url := fakeOllama(t, &down)
	dir := mustChdirTree(t)

	if err := RunIndex([]string{"--quiet", "--embed", "--ollama", url, "--chunk-size", "800", "."}); err == nil {
		t.Fatal("index --embed при недоступном сервисе прошёл, ожидалась ошибка")
	}

	var statusErr error
	withCapturedStdout(t, func() { statusErr = RunStatus([]string{"--json"}) })
	if statusErr != nil {
		t.Errorf("status после упавшего index --embed: %v", statusErr)
	}
	if code := runCheckIn(t, dir, "--quiet"); code != 3 {
		t.Errorf("check после упавшего index --embed: код %d, ожидался 3", code)
	}

	// Пустая база не закрепляет параметры неудачного запуска.
	if err := RunIndex([]string{"--quiet", "."}); err != nil {
		t.Fatalf("лексический index после упавшего index --embed: %v", err)
	}
	if code := runCheckIn(t, dir, "--quiet"); code != 0 {
		t.Errorf("check после лексического index: код %d, ожидался 0", code)
	}

	down = false
	if err := RunIndex([]string{"--quiet", "--embed", "--ollama", url, "--model", "test-model", "."}); err != nil {
		t.Fatalf("index --embed после восстановления сервиса: %v", err)
	}
	if model, dim := storedModel(t, dir); model != "test-model" || dim != 3 {
		t.Errorf("в базе модель %q, dim %d; ожидались test-model, 3", model, dim)
	}
}
