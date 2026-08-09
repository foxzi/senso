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
	if opts.Path != "" {
		t.Errorf("Path = %q, ожидалась пустая строка", opts.Path)
	}
	if opts.Ext != "" {
		t.Errorf("Ext = %q, ожидалась пустая строка", opts.Ext)
	}
	if opts.Exclude != "" {
		t.Errorf("Exclude = %q, ожидалась пустая строка", opts.Exclude)
	}
	if opts.Root != "" {
		t.Errorf("Root = %q, ожидалась пустая строка", opts.Root)
	}
}

// TestParseSearchArgsFilters проверяет, что --path/--ext/--exclude/--root
// разбираются в соответствующие поля searchOptions.
func TestParseSearchArgsFilters(t *testing.T) {
	opts, err := parseSearchArgs([]string{
		"--path", "internal/**,cmd/*",
		"--ext", "go,.md",
		"--exclude", "internal/generated/**",
		"--root", "/tmp/project",
		"запрос",
	})
	if err != nil {
		t.Fatalf("parseSearchArgs вернул ошибку: %v", err)
	}
	if opts.Path != "internal/**,cmd/*" {
		t.Errorf("Path = %q, ожидалось %q", opts.Path, "internal/**,cmd/*")
	}
	if opts.Ext != "go,.md" {
		t.Errorf("Ext = %q, ожидалось %q", opts.Ext, "go,.md")
	}
	if opts.Exclude != "internal/generated/**" {
		t.Errorf("Exclude = %q, ожидалось %q", opts.Exclude, "internal/generated/**")
	}
	if opts.Root != "/tmp/project" {
		t.Errorf("Root = %q, ожидалось %q", opts.Root, "/tmp/project")
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

func TestParseSearchArgsSemanticAndHybridConflict(t *testing.T) {
	_, err := parseSearchArgs([]string{"--semantic", "--hybrid", "запрос"})
	if err == nil {
		t.Fatal("parseSearchArgs с --semantic и --hybrid не вернул ошибку")
	}
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("ожидался *UsageError, получено %T: %v", err, err)
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

// TestFuseRRFPrefersDocumentInBothLists проверяет ключевое свойство RRF:
// документ, стоящий вторым сразу в обоих списках, должен обогнать документ,
// который занял первое место лишь в одном из них.
func TestFuseRRFPrefersDocumentInBothLists(t *testing.T) {
	lexical := []store.Result{
		{Path: "/only-lexical", Seq: 0},
		{Path: "/both", Seq: 0},
	}
	semantic := []store.Result{
		{Path: "/only-semantic", Seq: 0},
		{Path: "/both", Seq: 0},
	}

	got := fuseRRF([][]store.Result{lexical, semantic}, 10)

	if len(got) == 0 || got[0].Path != "/both" {
		t.Fatalf("fuseRRF[0] = %+v, ожидался документ /both первым", got)
	}
}

// TestFuseRRFDeterministicOnTies проверяет, что при равных суммарных
// вкладах порядок определяется по Path, затем по Seq.
func TestFuseRRFDeterministicOnTies(t *testing.T) {
	// Каждый ключ встречается ровно один раз на первом месте в своём
	// списке - все три получают одинаковый вклад RRF и становятся
	// "тем самым" случаем равенства сумм, который проверяет тест.
	lists := [][]store.Result{
		{{Path: "/b", Seq: 1}},
		{{Path: "/a", Seq: 2}},
		{{Path: "/a", Seq: 1}},
	}

	got := fuseRRF(lists, 10)

	want := []struct {
		path string
		seq  int
	}{
		{"/a", 1},
		{"/a", 2},
		{"/b", 1},
	}
	if len(got) != len(want) {
		t.Fatalf("fuseRRF вернул %d результатов, ожидалось %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Path != w.path || got[i].Seq != w.seq {
			t.Errorf("fuseRRF[%d] = %s#%d, ожидалось %s#%d", i, got[i].Path, got[i].Seq, w.path, w.seq)
		}
	}
}

// TestFuseRRFLimitsToK проверяет, что fuseRRF не возвращает больше k
// результатов, даже если объединённый пул кандидатов больше.
func TestFuseRRFLimitsToK(t *testing.T) {
	var list []store.Result
	for i := 0; i < 5; i++ {
		list = append(list, store.Result{Path: "/doc", Seq: i})
	}

	got := fuseRRF([][]store.Result{list}, 2)

	if len(got) != 2 {
		t.Fatalf("fuseRRF вернул %d результатов, ожидалось 2", len(got))
	}
}

// TestSearchPoolSizeWithoutFilter проверяет, что без активных фильтров
// пул совпадает с k без расширения.
func TestSearchPoolSizeWithoutFilter(t *testing.T) {
	f, _ := newResultFilter("", "", "", "", nil)
	if got := searchPoolSize(10, f); got != 10 {
		t.Errorf("searchPoolSize(10, неактивный фильтр) = %d, ожидалось 10", got)
	}
}

// TestSearchPoolSizeWithFilter проверяет расширение пула при активном
// фильтре: множитель filterPoolMultiplier и нижняя граница filterPoolMinimum.
func TestSearchPoolSizeWithFilter(t *testing.T) {
	f, err := newResultFilter("*.go", "", "", "", nil)
	if err != nil {
		t.Fatalf("newResultFilter вернул ошибку: %v", err)
	}

	if got := searchPoolSize(3, f); got != filterPoolMinimum {
		t.Errorf("searchPoolSize(3, активный фильтр) = %d, ожидалось %d (нижняя граница)", got, filterPoolMinimum)
	}
	if got := searchPoolSize(50, f); got != 50*filterPoolMultiplier {
		t.Errorf("searchPoolSize(50, активный фильтр) = %d, ожидалось %d", got, 50*filterPoolMultiplier)
	}
}

// TestFilterResultsKeepsOrder проверяет, что filterResults сохраняет
// исходный порядок отфильтрованных результатов.
func TestFilterResultsKeepsOrder(t *testing.T) {
	f, err := newResultFilter("", "go", "", "", nil)
	if err != nil {
		t.Fatalf("newResultFilter вернул ошибку: %v", err)
	}

	results := []store.Result{
		{Path: "/a.go", Seq: 0},
		{Path: "/b.md", Seq: 0},
		{Path: "/c.go", Seq: 0},
	}
	got := filterResults(results, f)

	want := []string{"/a.go", "/c.go"}
	if len(got) != len(want) {
		t.Fatalf("filterResults вернул %d результатов, ожидалось %d", len(got), len(want))
	}
	for i, w := range want {
		if got[i].Path != w {
			t.Errorf("filterResults[%d].Path = %q, ожидалось %q", i, got[i].Path, w)
		}
	}
}

// TestFilterResultsNoopWithoutFilter проверяет, что без активного фильтра
// filterResults возвращает исходный срез без изменений.
func TestFilterResultsNoopWithoutFilter(t *testing.T) {
	f, _ := newResultFilter("", "", "", "", nil)
	results := []store.Result{{Path: "/a"}, {Path: "/b"}}

	got := filterResults(results, f)
	if len(got) != len(results) {
		t.Fatalf("filterResults вернул %d результатов, ожидалось %d", len(got), len(results))
	}
}
