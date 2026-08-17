package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
)

// Файл содержит только вспомогательный код для сквозных тестов CLI:
// подготовку дерева файлов, разбор машинного вывода команд и подставной
// сервер эмбеддингов. Сами тесты лежат в e2e_test.go.

// writeTree создаёт во временном каталоге дерево файлов, где ключ карты -
// путь относительно корня (с прямыми слэшами), а значение - содержимое.
// Возвращает абсолютный путь к корню дерева.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, content := range files {
		writeFileIn(t, root, name, content)
	}
	return root
}

// writeFileIn записывает один файл внутрь дерева, создавая каталоги.
func writeFileIn(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// dbIn возвращает путь к файлу базы во отдельном временном каталоге, чтобы
// база не попадала в индексируемое дерево.
func dbIn(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "index.db")
}

// mustRun выполняет команду senso и требует нулевого кода завершения.
func mustRun(t *testing.T, args ...string) (stdout, stderr string) {
	t.Helper()
	code, stdout, stderr := runQuiet(t, args...)
	if code != exitOK {
		t.Fatalf("senso %s: код завершения = %d, ожидался %d\nstdout: %s\nstderr: %s",
			strings.Join(args, " "), code, exitOK, stdout, stderr)
	}
	return stdout, stderr
}

// decodeJSON разбирает машинный вывод команды в структуру dst.
func decodeJSON(t *testing.T, data string, dst any) {
	t.Helper()
	if err := json.Unmarshal([]byte(data), dst); err != nil {
		t.Fatalf("вывод не разбирается как JSON: %v\n%s", err, data)
	}
}

// indexReportJSON - подмножество полей отчёта index --report-json,
// достаточное для проверок в тестах.
type indexReportJSON struct {
	Scanned          int            `json:"scanned"`
	Indexed          int            `json:"indexed"`
	Updated          int            `json:"updated"`
	Unchanged        int            `json:"unchanged"`
	Deleted          int            `json:"deleted"`
	Chunks           int            `json:"chunks"`
	Skipped          int            `json:"skipped"`
	SkippedByCode    map[string]int `json:"skipped_by_code"`
	Excluded         int            `json:"excluded"`
	ExcludedByReason map[string]int `json:"excluded_by_reason"`
	Failed           []struct {
		Path    string `json:"path"`
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"failed"`
	Vectors bool `json:"vectors"`
}

// runIndexReport индексирует дерево и возвращает разобранный отчёт.
// Дополнительные флаги передаются перед путём.
func runIndexReport(t *testing.T, dbPath, root string, extra ...string) indexReportJSON {
	t.Helper()
	args := append([]string{"index", "--quiet", "--report-json", "--db", dbPath}, extra...)
	stdout, _ := mustRun(t, append(args, root)...)

	var rep indexReportJSON
	decodeJSON(t, stdout, &rep)
	return rep
}

// checkReportJSON - подмножество полей отчёта check --json.
type checkReportJSON struct {
	Fresh     bool   `json:"fresh"`
	Mode      string `json:"mode"`
	Scanned   int    `json:"scanned"`
	Unchanged int    `json:"unchanged"`
	Changed   int    `json:"changed"`
	Missing   int    `json:"missing"`
	Unindexed int    `json:"unindexed"`
	Excluded  int    `json:"excluded"`
	Issues    []struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"issues"`
	Vectors bool `json:"vectors"`
}

// runCheckJSON выполняет check --json и возвращает код завершения вместе с
// разобранным отчётом.
func runCheckJSON(t *testing.T, dbPath, root string, extra ...string) (int, checkReportJSON) {
	t.Helper()
	args := append([]string{"check", "--json", "--db", dbPath}, extra...)
	code, stdout, stderr := runQuiet(t, append(args, root)...)
	if code != exitOK && code != exitStale {
		t.Fatalf("senso check: неожиданный код завершения %d: %s", code, stderr)
	}

	var rep checkReportJSON
	decodeJSON(t, stdout, &rep)
	return code, rep
}

// searchResponse - подмножество полей ответа search --format json-v2.
type searchResponse struct {
	Schema  int    `json:"schema"`
	Mode    string `json:"mode"`
	Query   string `json:"query"`
	Results []struct {
		Ref        string  `json:"ref"`
		Path       string  `json:"path"`
		Chunk      int     `json:"chunk"`
		Rank       int     `json:"rank"`
		Score      float64 `json:"score"`
		ScoreKind  string  `json:"score_kind"`
		Text       string  `json:"text"`
		SourceType string  `json:"source_type"`
	} `json:"results"`
	Warnings []struct {
		Code string `json:"code"`
		Path string `json:"path"`
	} `json:"warnings"`
}

// runSearchV2 выполняет search --format json-v2 и возвращает разобранный ответ.
func runSearchV2(t *testing.T, dbPath, query string, extra ...string) searchResponse {
	t.Helper()
	args := append([]string{"search", "--db", dbPath, "--format", "json-v2"}, extra...)
	stdout, _ := mustRun(t, append(args, query)...)

	var resp searchResponse
	decodeJSON(t, stdout, &resp)
	if resp.Schema != 2 {
		t.Fatalf("schema = %d, ожидалось 2", resp.Schema)
	}
	return resp
}

// paths возвращает пути результатов в порядке выдачи, без повторов.
func (r searchResponse) paths() []string {
	var out []string
	seen := make(map[string]bool)
	for _, res := range r.Results {
		if seen[res.Path] {
			continue
		}
		seen[res.Path] = true
		out = append(out, res.Path)
	}
	return out
}

// names возвращает имена файлов из результатов (без каталогов), без повторов:
// в проверках важен состав выдачи, а не временные пути.
func (r searchResponse) names() []string {
	var out []string
	for _, p := range r.paths() {
		out = append(out, filepath.Base(p))
	}
	return out
}

// embedDim - размерность векторов подставного сервера эмбеддингов.
const embedDim = 16

// fakeEmbedVector строит детерминированный вектор из текста: гистограмма
// букв по embedDim корзинам. Похожие тексты дают близкие векторы, поэтому
// косинусный поиск по такому индексу осмысленно упорядочивает результаты.
func fakeEmbedVector(text string) []float32 {
	vec := make([]float32, embedDim)
	for _, r := range strings.ToLower(text) {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		vec[int(r)%embedDim]++
	}
	// Хотя бы одна ненулевая координата: иначе вектор нельзя нормализовать.
	vec[0]++
	return vec
}

// startFakeOllama поднимает HTTP-сервер, отвечающий на /api/embed так же,
// как Ollama, и возвращает его адрес. Сервер останавливается вместе с тестом.
func startFakeOllama(t *testing.T) string {
	t.Helper()

	mux := http.NewServeMux()
	mux.HandleFunc("/api/embed", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string   `json:"model"`
			Input []string `json:"input"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		resp := struct {
			Model      string      `json:"model"`
			Embeddings [][]float32 `json:"embeddings"`
		}{Model: req.Model}
		for _, text := range req.Input {
			resp.Embeddings = append(resp.Embeddings, fakeEmbedVector(text))
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(resp)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}
