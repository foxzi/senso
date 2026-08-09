package cli

import (
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// indexOptions хранит разобранные флаги и позиционный аргумент команды index.
type indexOptions struct {
	Path string

	DB          string
	Embed       bool
	Model       string
	Ext         string
	Exclude     string
	NoGitignore bool
	ChunkSize   int
	Overlap     int
	QueryPrefix string
	DocPrefix   string
	MaxFileSize int
	Concurrency int
	Prune       bool
	Ollama      string
	Quiet       bool
}

// parseIndexArgs разбирает аргументы командной строки подкоманды index.
func parseIndexArgs(args []string) (indexOptions, error) {
	var opts indexOptions

	defaultOllama := os.Getenv("OLLAMA_HOST")
	if defaultOllama == "" {
		defaultOllama = "http://localhost:11434"
	}

	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DB, "db", "", "путь к файлу базы данных")
	fs.BoolVar(&opts.Embed, "embed", false, "строить векторные эмбеддинги через Ollama (без флага индексация полностью локальная, лексическая, Ollama не требуется)")
	fs.StringVar(&opts.Model, "model", "bge-m3", "модель эмбеддингов в Ollama (действует только с --embed)")
	fs.StringVar(&opts.Ext, "ext", "", "список расширений файлов через запятую")
	fs.StringVar(&opts.Exclude, "exclude", "", "список glob-шаблонов исключений через запятую")
	fs.BoolVar(&opts.NoGitignore, "no-gitignore", false, "не учитывать .gitignore")
	fs.IntVar(&opts.ChunkSize, "chunk-size", 1200, "размер чанка в рунах")
	fs.IntVar(&opts.Overlap, "overlap", 150, "перекрытие чанков в рунах")
	fs.StringVar(&opts.QueryPrefix, "query-prefix", "", "префикс для поисковых запросов (действует только с --embed)")
	fs.StringVar(&opts.DocPrefix, "doc-prefix", "", "префикс для документов при индексации (действует только с --embed)")
	fs.IntVar(&opts.MaxFileSize, "max-file-size", 10, "максимальный размер файла в МБ")
	fs.IntVar(&opts.Concurrency, "concurrency", 4, "число параллельных обработчиков эмбеддинга (действует только с --embed)")
	fs.BoolVar(&opts.Prune, "prune", true, "удалять из индекса файлы, отсутствующие на диске")
	fs.StringVar(&opts.Ollama, "ollama", defaultOllama, "адрес сервера Ollama (действует только с --embed)")
	fs.BoolVar(&opts.Quiet, "quiet", false, "не выводить прогресс")

	if err := fs.Parse(args); err != nil {
		return indexOptions{}, finishParse(fs, err)
	}

	rest := fs.Args()
	switch len(rest) {
	case 0:
		opts.Path = "."
	case 1:
		opts.Path = rest[0]
	default:
		return indexOptions{}, usagef("index: ожидается не более одного позиционного аргумента (путь), получено %d", len(rest))
	}

	if opts.ChunkSize <= 0 {
		return indexOptions{}, usagef("--chunk-size должен быть больше 0")
	}
	if opts.Overlap < 0 {
		return indexOptions{}, usagef("--overlap не может быть отрицательным")
	}
	if opts.Overlap >= opts.ChunkSize {
		return indexOptions{}, usagef("--overlap должен быть меньше --chunk-size")
	}
	if opts.Concurrency <= 0 {
		return indexOptions{}, usagef("--concurrency должен быть больше 0")
	}
	if opts.MaxFileSize <= 0 {
		return indexOptions{}, usagef("--max-file-size должен быть больше 0")
	}

	return opts, nil
}

// splitList режет строку по запятым, обрезает пробелы вокруг элементов
// и отбрасывает пустые элементы. Для пустой строки возвращает nil.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var result []string
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		result = append(result, part)
	}
	return result
}

// normalizeExts приводит расширения файлов к единому виду: добавляет
// ведущую точку, если её нет, и переводит в нижний регистр.
func normalizeExts(list []string) []string {
	if len(list) == 0 {
		return nil
	}
	result := make([]string, 0, len(list))
	for _, ext := range list {
		ext = strings.ToLower(ext)
		if !strings.HasPrefix(ext, ".") {
			ext = "." + ext
		}
		result = append(result, ext)
	}
	return result
}

// matchesExt проверяет, соответствует ли расширение файла name одному
// из exts (регистронезависимо). Пустой список exts означает отсутствие
// фильтра, поэтому все файлы проходят проверку.
func matchesExt(name string, exts []string) bool {
	if len(exts) == 0 {
		return true
	}
	ext := filepath.Ext(name)
	for _, e := range exts {
		if strings.EqualFold(ext, e) {
			return true
		}
	}
	return false
}

// isNoisyName сообщает, является ли имя файла "шумным" - такие файлы
// исключаются из индексации всегда, даже если они текстовые.
func isNoisyName(name string) bool {
	base := strings.ToLower(filepath.Base(name))

	patterns := []string{
		"*.lock",
		"*-lock.json",
		"*.min.js",
		"*.min.css",
		"*.map",
		"*.svg",
	}
	for _, p := range patterns {
		if ok, _ := filepath.Match(p, base); ok {
			return true
		}
	}
	return false
}

// alwaysExcludedDir сообщает, исключается ли директория с данным именем
// всегда: служебные каталоги senso, .git, node_modules, vendor и любая
// скрытая директория (кроме самого текущего каталога ".").
func alwaysExcludedDir(name string) bool {
	switch name {
	case ".senso", ".git", "node_modules", "vendor":
		return true
	case ".":
		return false
	}
	if strings.HasPrefix(name, ".") {
		return true
	}
	return false
}
