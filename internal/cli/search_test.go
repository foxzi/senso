package cli

import (
	"errors"
	"testing"

	"senso/internal/store"
)

func TestParseSearchArgsDefaults(t *testing.T) {
	opts, err := parseSearchArgs([]string{"привет мир"})
	if err != nil {
		t.Fatalf("parseSearchArgs вернул ошибку: %v", err)
	}
	if opts.Query != "привет мир" {
		t.Errorf("Query = %q, ожидалось %q", opts.Query, "привет мир")
	}
	if opts.DB != "" {
		t.Errorf("DB = %q, ожидалась пустая строка", opts.DB)
	}
	if opts.K != 10 {
		t.Errorf("K = %d, ожидалось 10", opts.K)
	}
	if opts.JSON {
		t.Error("JSON = true, ожидалось false")
	}
	if opts.PathsOnly {
		t.Error("PathsOnly = true, ожидалось false")
	}
	if opts.Snippet != 500 {
		t.Errorf("Snippet = %d, ожидалось 500", opts.Snippet)
	}
	if opts.QueryPrefix != "" {
		t.Errorf("QueryPrefix = %q, ожидалась пустая строка", opts.QueryPrefix)
	}
	if opts.Semantic {
		t.Error("Semantic = true, ожидалось false")
	}
}

func TestParseSearchArgsSemantic(t *testing.T) {
	opts, err := parseSearchArgs([]string{"--semantic", "запрос"})
	if err != nil {
		t.Fatalf("parseSearchArgs вернул ошибку: %v", err)
	}
	if !opts.Semantic {
		t.Error("Semantic = false, ожидалось true")
	}
}

func TestParseSearchArgsJoinsPositional(t *testing.T) {
	opts, err := parseSearchArgs([]string{"как", "запустить", "проект"})
	if err != nil {
		t.Fatalf("parseSearchArgs вернул ошибку: %v", err)
	}
	if opts.Query != "как запустить проект" {
		t.Errorf("Query = %q, ожидалось %q", opts.Query, "как запустить проект")
	}
}

func TestParseSearchArgsEmptyQuery(t *testing.T) {
	if _, err := parseSearchArgs(nil); err == nil {
		t.Fatal("parseSearchArgs(nil) не вернул ошибку")
	}
	if _, err := parseSearchArgs([]string{"--k=5"}); err == nil {
		t.Fatal("parseSearchArgs без позиционных аргументов не вернул ошибку")
	}
}

func TestParseSearchArgsJSONAndPathsOnlyConflict(t *testing.T) {
	_, err := parseSearchArgs([]string{"--json", "--paths-only", "запрос"})
	if err == nil {
		t.Fatal("parseSearchArgs с --json и --paths-only не вернул ошибку")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("ожидался *UsageError, получено %T: %v", err, err)
	}
}

func TestParseSearchArgsInvalidK(t *testing.T) {
	cases := []string{"--k=0", "--k=-1"}
	for _, arg := range cases {
		if _, err := parseSearchArgs([]string{arg, "запрос"}); err == nil {
			t.Errorf("parseSearchArgs(%q) не вернул ошибку", arg)
		}
	}
}

func TestParseSearchArgsInvalidSnippet(t *testing.T) {
	if _, err := parseSearchArgs([]string{"--snippet=-1", "запрос"}); err == nil {
		t.Fatal("parseSearchArgs с отрицательным --snippet не вернул ошибку")
	}
}

func TestUniquePaths(t *testing.T) {
	results := []store.Result{
		{Path: "/a"},
		{Path: "/b"},
		{Path: "/a"},
		{Path: "/c"},
		{Path: "/b"},
		{Path: "/a"},
	}
	got := uniquePaths(results)
	want := []string{"/a", "/b", "/c"}
	if len(got) != len(want) {
		t.Fatalf("uniquePaths = %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("uniquePaths[%d] = %q, ожидалось %q", i, got[i], want[i])
		}
	}
}

func TestUniquePathsAllDuplicates(t *testing.T) {
	results := []store.Result{
		{Path: "/a"},
		{Path: "/a"},
		{Path: "/a"},
	}
	got := uniquePaths(results)
	if len(got) != 1 || got[0] != "/a" {
		t.Errorf("uniquePaths = %v, ожидалось [\"/a\"]", got)
	}
}

func TestUniquePathsEmpty(t *testing.T) {
	got := uniquePaths(nil)
	if len(got) != 0 {
		t.Errorf("uniquePaths(nil) = %v, ожидалась пустая последовательность", got)
	}
}

func TestFormatResultHeader(t *testing.T) {
	got := formatResultHeader("docs/setup.md", 3, 0, 0, 0.182)
	want := "docs/setup.md#3  0.182"
	if got != want {
		t.Errorf("formatResultHeader = %q, ожидалось %q", got, want)
	}
}

func TestFormatResultHeaderRounding(t *testing.T) {
	got := formatResultHeader("a.txt", 0, 0, 0, 0.1)
	want := "a.txt#0  0.100"
	if got != want {
		t.Errorf("formatResultHeader = %q, ожидалось %q", got, want)
	}
}

func TestFormatResultHeaderWithLineRange(t *testing.T) {
	got := formatResultHeader("docs/setup.md", 3, 40, 58, 0.182)
	want := "docs/setup.md#3  40-58  0.182"
	if got != want {
		t.Errorf("formatResultHeader = %q, ожидалось %q", got, want)
	}
}
