package cli

import (
	"errors"
	"testing"

	"senso/internal/store"
)

// TestDedupeResultsOverlappingLines проверяет основное правило дедупа:
// пересекающиеся по строкам чанки одного файла схлопываются в более
// релевантный (более ранний в списке), непересекающиеся остаются оба.
func TestDedupeResultsOverlappingLines(t *testing.T) {
	in := []store.Result{
		{Path: "/a.go", Seq: 0, StartLine: 1, EndLine: 20},
		// Пересекается с предыдущим (10-30 и 1-20 общая часть 10-20).
		{Path: "/a.go", Seq: 1, StartLine: 10, EndLine: 30},
		// Не пересекается ни с одним принятым результатом.
		{Path: "/a.go", Seq: 2, StartLine: 40, EndLine: 60},
	}

	got := dedupeResults(in)

	want := []int{0, 2}
	if len(got) != len(want) {
		t.Fatalf("dedupeResults вернул %d результатов, ожидалось %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Seq != w {
			t.Errorf("dedupeResults[%d].Seq = %d, ожидалось %d", i, got[i].Seq, w)
		}
	}
}

// TestDedupeResultsDifferentFilesNotMerged проверяет, что одинаковый текст
// (в данном случае - одинаковый диапазон строк) в разных файлах никогда не
// считается дубликатом.
func TestDedupeResultsDifferentFilesNotMerged(t *testing.T) {
	in := []store.Result{
		{Path: "/a.go", Seq: 0, StartLine: 1, EndLine: 20, Text: "same"},
		{Path: "/b.go", Seq: 0, StartLine: 1, EndLine: 20, Text: "same"},
	}

	got := dedupeResults(in)

	if len(got) != 2 {
		t.Fatalf("dedupeResults вернул %d результатов, ожидалось 2 (разные файлы): %+v", len(got), got)
	}
}

// TestDedupeResultsFallbackBySeq проверяет запасное правило: если у любого
// из результатов StartLine == 0 (данных о строках нет), дубликатами
// считаются соседние по номеру чанки (|Seq-Seq| <= 1).
func TestDedupeResultsFallbackBySeq(t *testing.T) {
	in := []store.Result{
		{Path: "/a.go", Seq: 5}, // StartLine == 0 - нет данных о строках
		{Path: "/a.go", Seq: 6}, // соседний чанк - дубликат
		{Path: "/a.go", Seq: 8}, // не соседний - остаётся
	}

	got := dedupeResults(in)

	want := []int{5, 8}
	if len(got) != len(want) {
		t.Fatalf("dedupeResults вернул %d результатов, ожидалось %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Seq != w {
			t.Errorf("dedupeResults[%d].Seq = %d, ожидалось %d", i, got[i].Seq, w)
		}
	}
}

// TestDedupeResultsFallbackMixedLineData проверяет запасное правило и
// тогда, когда данные о строках есть только у одного из пары результатов -
// по условию задачи достаточно, чтобы StartLine == 0 было хотя бы у одного.
func TestDedupeResultsFallbackMixedLineData(t *testing.T) {
	in := []store.Result{
		{Path: "/a.go", Seq: 0, StartLine: 100, EndLine: 120},
		{Path: "/a.go", Seq: 1}, // StartLine == 0, но Seq соседний - дубликат
	}

	got := dedupeResults(in)

	if len(got) != 1 || got[0].Seq != 0 {
		t.Fatalf("dedupeResults = %+v, ожидался один результат с Seq=0", got)
	}
}

// TestDedupeResultsInactive проверяет, что postProcessResults без
// --deduplicate возвращает результаты без изменений.
func TestDedupeResultsInactive(t *testing.T) {
	in := []store.Result{
		{Path: "/a.go", Seq: 0, StartLine: 1, EndLine: 20},
		{Path: "/a.go", Seq: 1, StartLine: 10, EndLine: 30},
	}

	got := postProcessResults(in, searchOptions{})

	if len(got) != len(in) {
		t.Fatalf("postProcessResults(без опций) вернул %d результатов, ожидалось %d", len(got), len(in))
	}
}

// TestLimitPerFileKeepsFirstN проверяет, что --max-per-file оставляет
// первые (наиболее релевантные) n результатов на файл и не меняет их
// относительный порядок.
func TestLimitPerFileKeepsFirstN(t *testing.T) {
	in := []store.Result{
		{Path: "/a.go", Seq: 0},
		{Path: "/a.go", Seq: 1},
		{Path: "/b.go", Seq: 0},
		{Path: "/a.go", Seq: 2},
		{Path: "/b.go", Seq: 1},
	}

	got := limitPerFile(in, 1)

	want := []struct {
		path string
		seq  int
	}{
		{"/a.go", 0},
		{"/b.go", 0},
	}
	if len(got) != len(want) {
		t.Fatalf("limitPerFile вернул %d результатов, ожидалось %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Path != w.path || got[i].Seq != w.seq {
			t.Errorf("limitPerFile[%d] = %s#%d, ожидалось %s#%d", i, got[i].Path, got[i].Seq, w.path, w.seq)
		}
	}
}

// TestLimitPerFileInactive проверяет, что max <= 0 не ограничивает выдачу.
func TestLimitPerFileInactive(t *testing.T) {
	in := []store.Result{
		{Path: "/a.go", Seq: 0},
		{Path: "/a.go", Seq: 1},
	}
	if got := limitPerFile(in, 0); len(got) != len(in) {
		t.Errorf("limitPerFile(..., 0) вернул %d результатов, ожидалось %d", len(got), len(in))
	}
}

// TestPostProcessResultsOrder проверяет совместное применение дедупа и
// --max-per-file в правильном порядке: сначала схлопываются перекрывающиеся
// чанки, затем оставшиеся урезаются до max-per-file. Порядок важен - если
// сначала применить max-per-file, результат мог бы отличаться.
func TestPostProcessResultsOrder(t *testing.T) {
	in := []store.Result{
		{Path: "/a.go", Seq: 0, StartLine: 1, EndLine: 20},
		// Пересекается с Seq=0 - будет отброшен дедупом. Если бы
		// max-per-file применялся первым (лимит 2), он остался бы,
		// заняв место непересекающегося результата ниже.
		{Path: "/a.go", Seq: 1, StartLine: 15, EndLine: 25},
		{Path: "/a.go", Seq: 2, StartLine: 40, EndLine: 60},
		{Path: "/a.go", Seq: 3, StartLine: 80, EndLine: 90},
	}

	got := postProcessResults(in, searchOptions{Deduplicate: true, MaxPerFile: 2})

	want := []int{0, 2}
	if len(got) != len(want) {
		t.Fatalf("postProcessResults вернул %d результатов, ожидалось %d: %+v", len(got), len(want), got)
	}
	for i, w := range want {
		if got[i].Seq != w {
			t.Errorf("postProcessResults[%d].Seq = %d, ожидалось %d", i, got[i].Seq, w)
		}
	}
}

// TestParseSearchArgsMaxPerFileNegative проверяет, что отрицательный
// --max-per-file - usage-ошибка (код выхода 2).
func TestParseSearchArgsMaxPerFileNegative(t *testing.T) {
	_, err := parseSearchArgs([]string{"--max-per-file", "-1", "запрос"})
	if err == nil {
		t.Fatal("parseSearchArgs с отрицательным --max-per-file должен вернуть ошибку")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("ожидался *UsageError, получено %T: %v", err, err)
	}
}

// TestParseSearchArgsDeduplicateAndMaxPerFile проверяет, что оба флага
// разбираются в соответствующие поля searchOptions.
func TestParseSearchArgsDeduplicateAndMaxPerFile(t *testing.T) {
	opts, err := parseSearchArgs([]string{"--deduplicate", "--max-per-file", "2", "запрос"})
	if err != nil {
		t.Fatalf("parseSearchArgs вернул ошибку: %v", err)
	}
	if !opts.Deduplicate {
		t.Error("Deduplicate = false, ожидалось true")
	}
	if opts.MaxPerFile != 2 {
		t.Errorf("MaxPerFile = %d, ожидалось 2", opts.MaxPerFile)
	}
}
