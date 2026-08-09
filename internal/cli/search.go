package cli

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"senso/internal/embed"
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

// parseSearchArgs разбирает аргументы командной строки подкоманды search.
func parseSearchArgs(args []string) (searchOptions, error) {
	var opts searchOptions

	defaultOllama := os.Getenv("OLLAMA_HOST")
	if defaultOllama == "" {
		defaultOllama = "http://localhost:11434"
	}

	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DB, "db", "", "путь к файлу базы данных")
	fs.IntVar(&opts.K, "k", 10, "число возвращаемых чанков")
	fs.BoolVar(&opts.JSON, "json", false, "вывести результаты в формате JSON")
	fs.BoolVar(&opts.PathsOnly, "paths-only", false, "вывести только уникальные пути файлов")
	fs.IntVar(&opts.Snippet, "snippet", 500, "длина фрагмента текста в результатах, в рунах")
	fs.BoolVar(&opts.Semantic, "semantic", false, "искать по векторам вместо лексического поиска (требует индекса, построенного с senso index --embed, и доступной Ollama)")
	fs.StringVar(&opts.Ollama, "ollama", defaultOllama, "адрес сервера Ollama (действует только с --semantic)")
	fs.StringVar(&opts.QueryPrefix, "query-prefix", "", "префикс для поискового запроса (действует только с --semantic); по умолчанию берётся префикс, сохранённый при индексации (senso index --embed --query-prefix)")

	if err := fs.Parse(args); err != nil {
		return searchOptions{}, finishParse(fs, err)
	}

	opts.Query = strings.Join(fs.Args(), " ")
	if opts.Query == "" {
		return searchOptions{}, usagef("search: требуется текст запроса")
	}
	if opts.K <= 0 {
		return searchOptions{}, usagef("-k должен быть больше 0")
	}
	if opts.Snippet < 0 {
		return searchOptions{}, usagef("--snippet не может быть отрицательным")
	}
	if opts.JSON && opts.PathsOnly {
		return searchOptions{}, usagef("--json и --paths-only нельзя использовать одновременно")
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
		return nil, fmt.Errorf("в базе нет векторов: запустите \"senso index --embed\" для их построения")
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
		return nil, fmt.Errorf("не удалось получить эмбеддинг запроса от Ollama (%s, модель %s): %w", opts.Ollama, model, err)
	}
	if len(vectors) != 1 {
		return nil, fmt.Errorf("Ollama вернула %d векторов для 1 запроса", len(vectors))
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
