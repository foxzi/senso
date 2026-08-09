package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"senso/internal/embed"
	"senso/internal/i18n"
	"senso/internal/store"
	"senso/internal/text"
)

// searchOptions хранит разобранные флаги и текст запроса команды search.
type searchOptions struct {
	Query string

	DB          string
	K           int
	JSON        bool
	PathsOnly   bool
	Snippet     int
	Semantic    bool
	Hybrid      bool
	Ollama      string
	QueryPrefix string
}

// searchFlagSet создаёт FlagSet подкоманды search, объявляет в opts все её
// флаги и возвращает FlagSet без вызова Parse. Используется как при разборе
// аргументов, так и при построении текста справки - это единственное место,
// где перечислены флаги search.
func searchFlagSet(opts *searchOptions) *flag.FlagSet {
	defaultOllama := os.Getenv("OLLAMA_HOST")
	if defaultOllama == "" {
		defaultOllama = "http://localhost:11434"
	}

	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DB, "db", "", i18n.T("path to the database file", "путь к файлу базы данных"))
	fs.IntVar(&opts.K, "k", 10, i18n.T("number of chunks to return", "число возвращаемых чанков"))
	fs.BoolVar(&opts.JSON, "json", false, i18n.T("print results as JSON", "вывести результаты в формате JSON"))
	fs.BoolVar(&opts.PathsOnly, "paths-only", false, i18n.T("print only unique file paths", "вывести только уникальные пути файлов"))
	fs.IntVar(&opts.Snippet, "snippet", 500, i18n.T("length of the text snippet in results, in runes", "длина фрагмента текста в результатах, в рунах"))
	fs.BoolVar(&opts.Semantic, "semantic", false, i18n.T("search by vectors instead of lexical search (requires an index built with senso index --embed, and Ollama available)", "искать по векторам вместо лексического поиска (требует индекса, построенного с senso index --embed, и доступной Ollama)"))
	// --hybrid объединяет лексический и семантический поиск через RRF (см.
	// fuseRRF). Score в результатах этого режима - ранг RRF, а не показатель
	// релевантности лексического или семантического поиска: сравнивать его
	// со score из других режимов нельзя.
	fs.BoolVar(&opts.Hybrid, "hybrid", false, i18n.T("combine lexical and semantic results (requires --embed index)", "объединить лексический и семантический результаты (нужен индекс с --embed)"))
	fs.StringVar(&opts.Ollama, "ollama", defaultOllama, i18n.T("Ollama server address (only applies with --semantic)", "адрес сервера Ollama (действует только с --semantic)"))
	fs.StringVar(&opts.QueryPrefix, "query-prefix", "", i18n.T("prefix for the search query (only applies with --semantic); defaults to the prefix saved during indexing (senso index --embed --query-prefix)", "префикс для поискового запроса (действует только с --semantic); по умолчанию берётся префикс, сохранённый при индексации (senso index --embed --query-prefix)"))
	return fs
}

// parseSearchArgs разбирает аргументы командной строки подкоманды search.
func parseSearchArgs(args []string) (searchOptions, error) {
	var opts searchOptions

	fs := searchFlagSet(&opts)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), commandHelpText(searchUsageLine(), searchSummary(), fs))
	}

	if err := fs.Parse(args); err != nil {
		return searchOptions{}, finishParse(fs, err)
	}

	opts.Query = strings.Join(fs.Args(), " ")
	if opts.Query == "" {
		return searchOptions{}, usagef("%s", i18n.T("search: query text is required", "search: требуется текст запроса"))
	}
	if opts.K <= 0 {
		return searchOptions{}, usagef("%s", i18n.T("-k must be greater than 0", "-k должен быть больше 0"))
	}
	if opts.Snippet < 0 {
		return searchOptions{}, usagef("%s", i18n.T("--snippet cannot be negative", "--snippet не может быть отрицательным"))
	}
	if opts.JSON && opts.PathsOnly {
		return searchOptions{}, usagef("%s", i18n.T("--json and --paths-only cannot be used together", "--json и --paths-only нельзя использовать одновременно"))
	}
	if opts.Semantic && opts.Hybrid {
		return searchOptions{}, usagef("%s", i18n.T("use either --semantic or --hybrid, not both", "укажите либо --semantic, либо --hybrid, но не оба"))
	}

	return opts, nil
}

// RunSearch реализует подкоманду "search": находит чанки, релевантные
// текстовому запросу, и печатает их в stdout. По умолчанию используется
// лексический поиск (без обращения к Ollama); с флагом --semantic - поиск
// по векторам.
func RunSearch(args []string) error {
	opts, err := parseSearchArgs(args)
	if err != nil {
		return err
	}

	s, _, err := openStore(opts.DB)
	if err != nil {
		return err
	}
	defer s.Close()

	var results []store.Result
	switch {
	case opts.Hybrid:
		results, err = searchHybrid(s, opts)
	case opts.Semantic:
		results, err = searchSemantic(s, opts, opts.K)
	default:
		results, err = s.SearchLexical(opts.Query, opts.K)
	}
	if err != nil {
		return err
	}

	switch {
	case opts.JSON:
		return printSearchJSON(results, opts.Query, opts.Snippet)
	case opts.PathsOnly:
		printSearchPaths(results)
	default:
		printSearchText(results, opts.Query, opts.Snippet)
	}
	return nil
}

// searchSemantic выполняет поиск по векторам: проверяет наличие векторов в
// базе, получает эмбеддинг запроса от Ollama и вызывает store.Search.
// k - число результатов, которое надо получить (для обычного семантического
// поиска это opts.K, для гибридного - расширенный пул кандидатов).
func searchSemantic(s *store.Store, opts searchOptions, k int) ([]store.Result, error) {
	hasVectors, err := s.HasVectors()
	if err != nil {
		return nil, err
	}
	if !hasVectors {
		return nil, errors.New(i18n.T("no vectors in the database: run \"senso index --embed\" to build them", "в базе нет векторов: запустите \"senso index --embed\" для их построения"))
	}

	model, _, err := s.Meta()
	if err != nil {
		return nil, err
	}

	queryPrefix := opts.QueryPrefix
	if queryPrefix == "" {
		queryPrefix, err = s.GetMeta("query_prefix")
		if err != nil {
			return nil, err
		}
	}

	client := embed.New(opts.Ollama, model)
	queryText := text.Normalize(queryPrefix + opts.Query)
	vectors, err := client.Embed(context.Background(), []string{queryText})
	if err != nil {
		return nil, fmt.Errorf(i18n.T("failed to get query embedding from ollama (%s, model %s): %w", "не удалось получить эмбеддинг запроса от Ollama (%s, модель %s): %w"), opts.Ollama, model, err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf(i18n.T("ollama returned %d vectors for 1 query", "Ollama вернула %d векторов для 1 запроса"), len(vectors))
	}
	vector := vectors[0]
	embed.Normalize(vector)

	return s.Search(vector, k)
}

// searchHybrid объединяет лексический и семантический поиск с помощью
// Reciprocal Rank Fusion: у каждого источника запрашивается расширенный пул
// кандидатов, чтобы слияние не упиралось в то, что итоговые top-K одного
// метода не пересекаются с top-K другого.
func searchHybrid(s *store.Store, opts searchOptions) ([]store.Result, error) {
	pool := opts.K * 4
	if pool < 50 {
		pool = 50
	}

	lexical, err := s.SearchLexical(opts.Query, pool)
	if err != nil {
		return nil, err
	}
	semantic, err := searchSemantic(s, opts, pool)
	if err != nil {
		return nil, err
	}

	return fuseRRF([][]store.Result{lexical, semantic}, opts.K), nil
}

// rrfK - сглаживающая константа Reciprocal Rank Fusion. Значение 60 взято
// из исходной статьи Cormack et al. и на практике почти не требует подбора.
const rrfK = 60

// fuseRRF объединяет несколько ранжированных списков результатов методом
// Reciprocal Rank Fusion: вклад результата на позиции i (0-based) любого
// списка равен 1/(rrfK + i + 1), вклады по спискам суммируются. Результаты
// сопоставляются по паре (Path, Seq). Итог сортируется по убыванию
// суммарного вклада, при равенстве - по Path, затем по Seq, для
// детерминированности порядка. Возвращается не более k результатов; в поле
// Score записывается суммарный вклад RRF - ранговый показатель, несравнимый
// со score чисто лексического или чисто семантического поиска.
func fuseRRF(lists [][]store.Result, k int) []store.Result {
	type key struct {
		path string
		seq  int
	}

	fused := make(map[key]store.Result)
	scores := make(map[key]float64)

	for _, list := range lists {
		for i, r := range list {
			kk := key{path: r.Path, seq: r.Seq}
			if _, ok := fused[kk]; !ok {
				fused[kk] = r
			}
			scores[kk] += 1 / float64(rrfK+i+1)
		}
	}

	out := make([]store.Result, 0, len(fused))
	for kk, r := range fused {
		r.Score = scores[kk]
		out = append(out, r)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Seq < out[j].Seq
	})

	if len(out) > k {
		out = out[:k]
	}
	return out
}

// formatResultHeader форматирует заголовочную строку одного результата:
// путь с номером чанка через "#", диапазон строк исходного файла и
// показатель релевантности с тремя знаками после запятой (больше -
// релевантнее). Если startLine == 0 (данных о строках нет), диапазон не
// печатается.
func formatResultHeader(path string, seq, startLine, endLine int, score float64) string {
	if startLine == 0 {
		return fmt.Sprintf("%s#%d  %.3f", path, seq, score)
	}
	return fmt.Sprintf("%s#%d  %d-%d  %.3f", path, seq, startLine, endLine, score)
}

// printSearchText печатает результаты в человекочитаемом виде: путь
// сокращается относительно текущей директории, текст обрезается окном
// сниппета вокруг слова запроса. Сниппет может быть многострочным, поэтому
// отступ ставится перед каждой его строкой.
func printSearchText(results []store.Result, query string, snippetLen int) {
	cwd, _ := os.Getwd()
	for _, r := range results {
		path := shortenPath(r.Path, cwd)
		fmt.Println(formatResultHeader(path, r.Seq, r.StartLine, r.EndLine, r.Score))
		snippet := snippetAround(r.Text, query, snippetLen)
		for _, line := range strings.Split(snippet, "\n") {
			fmt.Printf("    %s\n", line)
		}
	}
}

// searchResultJSON - структура одного результата для вывода в формате JSON.
type searchResultJSON struct {
	Path      string  `json:"path"`
	Chunk     int     `json:"chunk"`
	StartLine int     `json:"line_start"`
	EndLine   int     `json:"line_end"`
	Score     float64 `json:"score"`
	Text      string  `json:"text"`
}

// printSearchJSON печатает результаты как JSON-массив объектов.
// Пути в выводе абсолютные, текст обрезается окном сниппета вокруг слова
// запроса.
func printSearchJSON(results []store.Result, query string, snippetLen int) error {
	out := make([]searchResultJSON, 0, len(results))
	for _, r := range results {
		out = append(out, searchResultJSON{
			Path:      r.Path,
			Chunk:     r.Seq,
			StartLine: r.StartLine,
			EndLine:   r.EndLine,
			Score:     r.Score,
			Text:      snippetAround(r.Text, query, snippetLen),
		})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// printSearchPaths печатает уникальные абсолютные пути файлов, по одному на
// строку, в порядке первого появления в results.
func printSearchPaths(results []store.Result) {
	for _, p := range uniquePaths(results) {
		fmt.Println(p)
	}
}

// uniquePaths схлопывает результаты поиска в список уникальных путей,
// сохраняя порядок их первого появления.
func uniquePaths(results []store.Result) []string {
	seen := make(map[string]bool, len(results))
	paths := make([]string, 0, len(results))
	for _, r := range results {
		if seen[r.Path] {
			continue
		}
		seen[r.Path] = true
		paths = append(paths, r.Path)
	}
	return paths
}
