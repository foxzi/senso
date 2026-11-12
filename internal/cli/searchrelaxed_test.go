package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// searchV2 выполняет search --format json-v2 в текущем каталоге и
// возвращает разобранный ответ.
func searchV2(t *testing.T, args ...string) searchResponseV2 {
	t.Helper()
	var resp searchResponseV2
	out := withCapturedStdout(t, func() {
		if err := RunSearch(append([]string{"--format", "json-v2"}, args...)); err != nil {
			t.Fatalf("RunSearch(%v): %v", args, err)
		}
	})
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("разбор ответа: %v\n%s", err, out)
	}
	return resp
}

// Если ни один чанк не содержит всех слов запроса, поиск повторяется с
// любым из слов и помечает выдачу как смягчённую.
func TestSearchRelaxesQueryWithoutMatches(t *testing.T) {
	dir := mustChdirTree(t)
	for name, text := range map[string]string{
		"uploads.md": "large uploads are limited by the proxy",
		"other.md":   "uploads go to object storage",
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := RunIndex([]string{"--quiet", "."}); err != nil {
		t.Fatalf("RunIndex: %v", err)
	}

	t.Run("все слова найдены - запрос строгий", func(t *testing.T) {
		resp := searchV2(t, "large uploads")
		if resp.Relaxed || len(resp.Results) != 1 {
			t.Errorf("relaxed=%v, результатов %d; ожидалось false и 1", resp.Relaxed, len(resp.Results))
		}
	})

	t.Run("лишнее слово - запрос смягчается", func(t *testing.T) {
		resp := searchV2(t, "large uploads nginx")
		if !resp.Relaxed {
			t.Error("relaxed=false, ожидалось true")
		}
		if len(resp.Results) != 2 {
			t.Fatalf("результатов %d, ожидалось 2", len(resp.Results))
		}
		if got := filepath.Base(resp.Results[0].Path); got != "uploads.md" {
			t.Errorf("первый результат %q, ожидался uploads.md: в нём совпало больше слов", got)
		}
	})

	t.Run("смягчённая выдача проходит через фильтры", func(t *testing.T) {
		resp := searchV2(t, "--path", "other.md", "large uploads nginx")
		if !resp.Relaxed || len(resp.Results) != 1 || filepath.Base(resp.Results[0].Path) != "other.md" {
			t.Errorf("relaxed=%v, результаты %+v; ожидался один other.md", resp.Relaxed, resp.Results)
		}
	})

	t.Run("ни одно слово не найдено - пустая выдача без пометки", func(t *testing.T) {
		resp := searchV2(t, "nginx kubernetes")
		if resp.Relaxed || len(resp.Results) != 0 {
			t.Errorf("relaxed=%v, результатов %d; ожидалось false и 0", resp.Relaxed, len(resp.Results))
		}
	})
}
