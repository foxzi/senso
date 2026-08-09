package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
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
	if opts.Semantic {
		results, err = searchSemantic(s, opts)
	} else {
		results, err = s.SearchLexical(opts.Query, opts.K)
	}
	if err != nil {
		return err
	}

	switch {
	case opts.JSON:
		return printSearchJSON(results, opts.Snippet)
	case opts.PathsOnly:
		printSearchPaths(results)
	default:
		printSearchText(results, opts.Snippet)
	}
	return nil
}

// searchSemantic выполняет поиск по векторам: проверяет наличие векторов в
// базе, получает эмбеддинг запроса от Ollama и вызывает store.Search.
func searchSemantic(s *store.Store, opts searchOptions) ([]store.Result, error) {
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

	return s.Search(vector, opts.K)
}

// formatResultHeader форматирует заголовочную строку одного результата:
// путь с номером чанка через "#" и показатель релевантности с тремя знаками
// после запятой (больше - релевантнее).
func formatResultHeader(path string, seq int, score float64) string {
	return fmt.Sprintf("%s#%d  %.3f", path, seq, score)
}

// printSearchText печатает результаты в человекочитаемом виде: путь
// сокращается относительно текущей директории, текст обрезается snippet'ом.
func printSearchText(results []store.Result, snippetLen int) {
	cwd, _ := os.Getwd()
	for _, r := range results {
		path := shortenPath(r.Path, cwd)
		fmt.Println(formatResultHeader(path, r.Seq, r.Score))
		fmt.Printf("    %s\n", snippet(r.Text, snippetLen))
	}
}

// searchResultJSON - структура одного результата для вывода в формате JSON.
type searchResultJSON struct {
	Path  string  `json:"path"`
	Chunk int     `json:"chunk"`
	Score float64 `json:"score"`
	Text  string  `json:"text"`
}

// printSearchJSON печатает результаты как JSON-массив объектов.
// Пути в выводе абсолютные, текст обрезается snippet'ом.
func printSearchJSON(results []store.Result, snippetLen int) error {
	out := make([]searchResultJSON, 0, len(results))
	for _, r := range results {
		out = append(out, searchResultJSON{
			Path:  r.Path,
			Chunk: r.Seq,
			Score: r.Score,
			Text:  snippet(r.Text, snippetLen),
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
