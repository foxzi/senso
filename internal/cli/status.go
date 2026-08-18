package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"senso/internal/i18n"
	"senso/internal/store"
)

// statusOptions хранит разобранные флаги команды status.
type statusOptions struct {
	DB   string
	JSON bool
}

// statusFlagSet создаёт FlagSet подкоманды status, объявляет в opts все её
// флаги и возвращает FlagSet без вызова Parse. Используется как при разборе
// аргументов, так и при построении текста справки - это единственное место,
// где перечислены флаги status.
func statusFlagSet(opts *statusOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DB, "db", "", i18n.T("path to the database file", "путь к файлу базы данных"))
	fs.BoolVar(&opts.JSON, "json", false, i18n.T("print statistics as JSON", "вывести статистику в формате JSON"))
	return fs
}

// RunStatus реализует подкоманду "status": печатает сводную информацию
// об индексе - путь к базе, корень, модель, размер и время индексации.
func RunStatus(args []string) error {
	var opts statusOptions
	fs := statusFlagSet(&opts)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), commandHelpText(statusUsageLine(), statusSummary(), fs))
	}
	if err := fs.Parse(args); err != nil {
		return finishParse(fs, err)
	}
	if fs.NArg() != 0 {
		return usagef(i18n.T("status: unknown arguments: %v", "status: неизвестные аргументы: %v"), fs.Args())
	}

	s, path, err := openStore(opts.DB)
	if err != nil {
		return err
	}
	defer s.Close()

	stats, err := s.Stats()
	if err != nil {
		return err
	}

	var dbSize int64
	if info, err := os.Stat(path); err == nil {
		dbSize = info.Size()
	}

	// Параметры нарезки нужны, чтобы повторную индексацию можно было
	// запустить теми же флагами: index отвергает другие. В базах, созданных
	// до появления этих ключей, их нет - тогда строка просто не печатается.
	params, _, _ := loadChunkParams(s)

	if opts.JSON {
		return printStatusJSON(path, dbSize, stats, params)
	}
	printStatusText(path, dbSize, stats, params)
	return nil
}

// statusJSON - структура для вывода статистики в формате JSON.
type statusJSON struct {
	DB        string         `json:"db"`
	Root      string         `json:"root"`
	Mode      string         `json:"mode"`
	Model     string         `json:"model"`
	Dim       int            `json:"dim"`
	Files     int            `json:"files"`
	Chunks    int            `json:"chunks"`
	FTSRows   int            `json:"fts_rows"`
	Vectors   int            `json:"vectors"`
	SizeBytes int64          `json:"size_bytes"`
	IndexedAt string         `json:"indexed_at"`
	Chunker   string         `json:"chunker,omitempty"`
	ChunkSize int            `json:"chunk_size,omitempty"`
	Overlap   int            `json:"overlap,omitempty"`
	Roots     map[string]int `json:"roots,omitempty"`
}

// printStatusJSON печатает статистику одним JSON-объектом.
func printStatusJSON(path string, dbSize int64, stats store.Stats, params chunkParams) error {
	out := statusJSON{
		DB:        path,
		Root:      stats.Root,
		Mode:      indexModeCode(stats),
		Model:     stats.Model,
		Dim:       stats.Dim,
		Files:     stats.Files,
		Chunks:    stats.Chunks,
		FTSRows:   stats.FTSRows,
		Vectors:   stats.Vectors,
		SizeBytes: dbSize,
		IndexedAt: stats.IndexedAt,
		Chunker:   params.Chunker,
		ChunkSize: params.ChunkSize,
		Overlap:   params.Overlap,
		Roots:     stats.Roots,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

// indexModeCode возвращает машиночитаемый код режима индекса для JSON:
// "lexical" либо "lexical+semantic" (при наличии векторов).
func indexModeCode(stats store.Stats) string {
	if stats.Vectors > 0 {
		return "lexical+semantic"
	}
	return "lexical"
}

// indexModeText возвращает человекочитаемое описание режима индекса:
// с векторами (семантический поиск) или без (только лексический).
func indexModeText(stats store.Stats) string {
	if stats.Vectors > 0 {
		return i18n.T("lexical and semantic", "лексический и семантический")
	}
	return i18n.T("lexical only", "только лексический")
}

// printStatusText печатает статистику в человекочитаемом текстовом виде.
func printStatusText(path string, dbSize int64, stats store.Stats, params chunkParams) {
	fmt.Printf(i18n.T("database:       %s\n", "база данных:    %s\n"), path)
	fmt.Printf(i18n.T("root:           %s\n", "корень:         %s\n"), stats.Root)
	fmt.Printf(i18n.T("mode:           %s\n", "режим:          %s\n"), indexModeText(stats))
	if stats.Model != "" {
		fmt.Printf(i18n.T("model:          %s\n", "модель:         %s\n"), stats.Model)
		fmt.Printf(i18n.T("dimensions:     %d\n", "размерность:    %d\n"), stats.Dim)
	}
	fmt.Printf(i18n.T("files:          %d\n", "файлов:         %d\n"), stats.Files)
	fmt.Printf(i18n.T("chunks:         %d\n", "чанков:         %d\n"), stats.Chunks)
	fmt.Printf(i18n.T("fts rows:       %d\n", "лексич. строк:  %d\n"), stats.FTSRows)
	if stats.Vectors > 0 {
		fmt.Printf(i18n.T("vectors:        %d\n", "векторов:       %d\n"), stats.Vectors)
	}
	if params.Chunker != "" {
		fmt.Printf(i18n.T("chunking:       %s, size %d, overlap %d\n", "нарезка:        %s, размер %d, перекрытие %d\n"),
			params.Chunker, params.ChunkSize, params.Overlap)
	}
	fmt.Printf(i18n.T("db size:        %s\n", "размер базы:    %s\n"), humanSize(dbSize))
	fmt.Printf(i18n.T("indexed at:     %s\n", "индексировано:  %s\n"), stats.IndexedAt)

	if len(stats.Roots) > 1 {
		fmt.Println(i18n.T("roots:", "корни:"))
		keys := make([]string, 0, len(stats.Roots))
		for k := range stats.Roots {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Printf("  %-40s %d\n", k, stats.Roots[k])
		}
	}
}

// humanSize форматирует размер в байтах в человекочитаемый вид
// (например, "12.4 MB").
func humanSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	units := []string{"KB", "MB", "GB", "TB"}
	return fmt.Sprintf("%.1f %s", float64(bytes)/float64(div), units[exp])
}
