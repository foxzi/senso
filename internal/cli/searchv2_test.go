package cli

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"

	"senso/internal/store"
)

func TestSearchOutputFormatDefaultsAndAliases(t *testing.T) {
	tests := []struct {
		name string
		opts searchOptions
		want string
	}{
		{"без флагов", searchOptions{}, formatText},
		{"--json", searchOptions{JSON: true}, formatJSON},
		{"--paths-only", searchOptions{PathsOnly: true}, formatPaths},
		{"--format json-v2", searchOptions{Format: formatJSONV2}, formatJSONV2},
		{"--format text", searchOptions{Format: formatText}, formatText},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.opts.outputFormat(); got != tt.want {
				t.Errorf("outputFormat() = %q, ожидалось %q", got, tt.want)
			}
		})
	}
}

func TestParseSearchArgsUnknownFormat(t *testing.T) {
	_, err := parseSearchArgs([]string{"--format", "yaml", "query"})
	if err == nil {
		t.Fatal("parseSearchArgs с неизвестным --format не вернул ошибку")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("ожидался *UsageError, получено %T: %v", err, err)
	}
}

func TestParseSearchArgsFormatConflictsWithJSON(t *testing.T) {
	for _, alias := range []string{"--json", "--paths-only"} {
		t.Run(alias, func(t *testing.T) {
			_, err := parseSearchArgs([]string{"--format", formatJSON, alias, "query"})
			if err == nil {
				t.Fatalf("parseSearchArgs с --format и %s не вернул ошибку", alias)
			}
			var usageErr *UsageError
			if !errors.As(err, &usageErr) {
				t.Errorf("ожидался *UsageError, получено %T: %v", err, err)
			}
		})
	}
}

func TestSearchModeAndScoreKind(t *testing.T) {
	tests := []struct {
		name      string
		opts      searchOptions
		wantMode  string
		wantScore string
	}{
		{"лексический", searchOptions{}, modeLexical, scoreBM25},
		{"семантический", searchOptions{Semantic: true}, modeSemantic, scoreCosine},
		{"гибридный", searchOptions{Hybrid: true}, modeHybrid, scoreRRF},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := searchMode(tt.opts); got != tt.wantMode {
				t.Errorf("searchMode() = %q, ожидалось %q", got, tt.wantMode)
			}
			if got := searchScoreKind(tt.opts); got != tt.wantScore {
				t.Errorf("searchScoreKind() = %q, ожидалось %q", got, tt.wantScore)
			}
		})
	}
}

func TestSourceTypeFor(t *testing.T) {
	if got := sourceTypeFor("/tmp/a.go"); got != sourceText {
		t.Errorf("sourceTypeFor(.go) = %q, ожидалось %q", got, sourceText)
	}
	if got := sourceTypeFor("/tmp/report.docx"); got != sourceExtracted {
		t.Errorf("sourceTypeFor(.docx) = %q, ожидалось %q", got, sourceExtracted)
	}
}

func TestBuildSearchFiltersV2OmitsInactiveFilters(t *testing.T) {
	filter, err := newResultFilter("", "", "", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	got := buildSearchFiltersV2(searchOptions{K: 5}, filter)

	if got.K != 5 {
		t.Errorf("K = %d, ожидалось 5", got.K)
	}
	if got.Path != nil || got.Ext != nil || got.Exclude != nil || got.Root != "" {
		t.Errorf("неактивные фильтры попали в ответ: %+v", got)
	}
	if got.Deduplicate || got.MaxPerFile != 0 {
		t.Errorf("выключенная дедупликация попала в ответ: %+v", got)
	}
}

func TestBuildSearchFiltersV2ReportsAppliedFilters(t *testing.T) {
	filter, err := newResultFilter("cmd/**", "go, md", "**/testdata/**", "", nil)
	if err != nil {
		t.Fatal(err)
	}

	got := buildSearchFiltersV2(searchOptions{K: 3, Deduplicate: true, MaxPerFile: 2}, filter)

	if len(got.Path) != 1 || got.Path[0] != "cmd/**" {
		t.Errorf("Path = %v, ожидалось [cmd/**]", got.Path)
	}
	if len(got.Ext) != 2 || got.Ext[0] != ".go" || got.Ext[1] != ".md" {
		t.Errorf("Ext = %v, ожидались нормализованные расширения [.go .md]", got.Ext)
	}
	if len(got.Exclude) != 1 || got.Exclude[0] != "**/testdata/**" {
		t.Errorf("Exclude = %v, ожидалось [**/testdata/**]", got.Exclude)
	}
	if !got.Deduplicate || got.MaxPerFile != 2 {
		t.Errorf("Deduplicate/MaxPerFile = %v/%d, ожидались true/2", got.Deduplicate, got.MaxPerFile)
	}
}

func TestBuildSearchResponseV2FillsRankRefAndTruncation(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	results := []store.Result{
		{Path: filePath, Seq: 0, Text: "chunk text 0", Score: 1.5, StartLine: 1, EndLine: 1},
		{Path: filePath, Seq: 3, Text: "chunk text 3", Score: 0.5, StartLine: 4, EndLine: 4},
	}
	opts := searchOptions{Query: "chunk", K: 2, Snippet: 5}

	resp, err := buildSearchResponseV2(context.Background(), s, results, opts, nil)
	if err != nil {
		t.Fatalf("buildSearchResponseV2 вернул ошибку: %v", err)
	}

	if resp.Schema != searchSchemaV2 {
		t.Errorf("Schema = %d, ожидалось %d", resp.Schema, searchSchemaV2)
	}
	if resp.Mode != modeLexical || resp.Query != "chunk" {
		t.Errorf("Mode/Query = %q/%q, ожидались lexical/chunk", resp.Mode, resp.Query)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("получено %d результатов, ожидалось 2", len(resp.Results))
	}
	if resp.Results[0].Rank != 1 || resp.Results[1].Rank != 2 {
		t.Errorf("ранги = %d/%d, ожидались 1/2", resp.Results[0].Rank, resp.Results[1].Rank)
	}
	if resp.Results[1].Ref != filePath+"#3" {
		t.Errorf("Ref = %q, ожидался %s#3", resp.Results[1].Ref, filePath)
	}
	if resp.Results[0].ScoreKind != scoreBM25 {
		t.Errorf("ScoreKind = %q, ожидался %q", resp.Results[0].ScoreKind, scoreBM25)
	}
	if !resp.Results[0].Truncated {
		t.Error("Truncated = false, хотя текст обрезан окном --snippet")
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("Warnings = %v, ожидался пустой список для свежего файла", resp.Warnings)
	}
}

func TestBuildSearchResponseV2WarnsOncePerChangedFile(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	if err := os.WriteFile(filePath, []byte("stub changed on disk"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	results := []store.Result{
		{Path: filePath, Seq: 0, Text: "chunk text 0"},
		{Path: filePath, Seq: 1, Text: "chunk text 1"},
	}

	resp, err := buildSearchResponseV2(context.Background(), s, results, searchOptions{Query: "chunk", K: 2}, nil)
	if err != nil {
		t.Fatalf("buildSearchResponseV2 вернул ошибку: %v", err)
	}

	if len(resp.Warnings) != 1 {
		t.Fatalf("получено %d предупреждений, ожидалось одно на файл: %+v", len(resp.Warnings), resp.Warnings)
	}
	if resp.Warnings[0].Code != warnFileModified {
		t.Errorf("код предупреждения = %q, ожидался %q", resp.Warnings[0].Code, warnFileModified)
	}
	if resp.Warnings[0].Path != filePath {
		t.Errorf("путь предупреждения = %q, ожидался %q", resp.Warnings[0].Path, filePath)
	}
}

func TestBuildSearchResponseV2WarnsOnMissingFile(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)
	if err := os.Remove(filePath); err != nil {
		t.Fatal(err)
	}
	s, err := store.Open(context.Background(), dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	resp, err := buildSearchResponseV2(context.Background(), s, []store.Result{{Path: filePath, Text: "chunk text 0"}}, searchOptions{K: 1}, nil)
	if err != nil {
		t.Fatalf("buildSearchResponseV2 вернул ошибку: %v", err)
	}

	if len(resp.Warnings) != 1 || resp.Warnings[0].Code != warnFileMissing {
		t.Fatalf("предупреждения = %+v, ожидалось одно с кодом %q", resp.Warnings, warnFileMissing)
	}
}

func TestRunSearchJSONV2IsSingleObject(t *testing.T) {
	dbPath, filePath := mustBuildShowIndex(t)

	out := withCapturedStdout(t, func() {
		if err := RunSearch([]string{"--db", dbPath, "--format", formatJSONV2, "-k", "3", "chunk"}); err != nil {
			t.Fatalf("RunSearch вернул ошибку: %v", err)
		}
	})

	var resp searchResponseV2
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("вывод не разбирается как json-v2: %v\n%s", err, out)
	}
	if resp.Schema != searchSchemaV2 {
		t.Errorf("Schema = %d, ожидалось %d", resp.Schema, searchSchemaV2)
	}
	if resp.Filters.K != 3 {
		t.Errorf("Filters.K = %d, ожидалось 3", resp.Filters.K)
	}
	if len(resp.Results) == 0 {
		t.Fatalf("json-v2 не содержит результатов: %s", out)
	}
	if !strings.HasPrefix(resp.Results[0].Ref, filePath+"#") {
		t.Errorf("Ref = %q, ожидался префикс %s#", resp.Results[0].Ref, filePath)
	}
	if resp.Results[0].Rank != 1 {
		t.Errorf("Rank первого результата = %d, ожидался 1", resp.Results[0].Rank)
	}
}
