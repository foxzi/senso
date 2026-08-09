package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"

	"senso/internal/store"
)

// RunStatus реализует подкоманду "status": печатает сводную информацию
// об индексе - путь к базе, корень, модель, размер и время индексации.
func RunStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	dbFlag := fs.String("db", "", "путь к файлу базы данных")
	jsonFlag := fs.Bool("json", false, "вывести статистику в формате JSON")
	if err := fs.Parse(args); err != nil {
		return finishParse(fs, err)
	}
	if fs.NArg() != 0 {
		return usagef("status: неизвестные аргументы: %v", fs.Args())
	}

	s, path, err := openStore(*dbFlag)
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

	if *jsonFlag {
		return printStatusJSON(path, dbSize, stats)
	}
	printStatusText(path, dbSize, stats)
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
	Roots     map[string]int `json:"roots,omitempty"`
}

// printStatusJSON печатает статистику одним JSON-объектом.
func printStatusJSON(path string, dbSize int64, stats store.Stats) error {
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
		return "лексический и семантический"
	}
	return "только лексический"
}

// printStatusText печатает статистику в человекочитаемом текстовом виде.
func printStatusText(path string, dbSize int64, stats store.Stats) {
	fmt.Printf("база данных:    %s\n", path)
	fmt.Printf("корень:         %s\n", stats.Root)
	fmt.Printf("режим:          %s\n", indexModeText(stats))
	if stats.Model != "" {
		fmt.Printf("модель:         %s\n", stats.Model)
		fmt.Printf("размерность:    %d\n", stats.Dim)
	}
	fmt.Printf("файлов:         %d\n", stats.Files)
	fmt.Printf("чанков:         %d\n", stats.Chunks)
	fmt.Printf("лексич. строк:  %d\n", stats.FTSRows)
	if stats.Vectors > 0 {
		fmt.Printf("векторов:       %d\n", stats.Vectors)
	}
	fmt.Printf("размер базы:    %s\n", humanSize(dbSize))
	fmt.Printf("индексировано:  %s\n", stats.IndexedAt)

	if len(stats.Roots) > 1 {
		fmt.Println("корни:")
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
