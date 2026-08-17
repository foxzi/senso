package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"senso/internal/i18n"
)

// indexOptions хранит разобранные флаги и позиционный аргумент команды index.
type indexOptions struct {
	Path string

	DB            string
	Embed         bool
	Model         string
	Ext           string
	Exclude       string
	NoGitignore   bool
	Hidden        bool
	IncludeHidden string
	Noisy         bool
	IncludeNoisy  string
	NoisyPatterns string
	ChunkSize     int
	Overlap       int
	QueryPrefix   string
	DocPrefix     string
	MaxFileSize   int
	Concurrency   int
	Prune         bool
	Ollama        string
	Quiet         bool
}

// indexFlagSet создаёт FlagSet подкоманды index, объявляет в opts все её
// флаги и возвращает FlagSet без вызова Parse. Используется как при разборе
// аргументов, так и при построении текста справки - это единственное место,
// где перечислены флаги index.
func indexFlagSet(opts *indexOptions) *flag.FlagSet {
	defaultOllama := os.Getenv("OLLAMA_HOST")
	if defaultOllama == "" {
		defaultOllama = "http://localhost:11434"
	}

	fs := flag.NewFlagSet("index", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DB, "db", "", i18n.T("path to the database file", "путь к файлу базы данных"))
	fs.BoolVar(&opts.Embed, "embed", false, i18n.T("build vector embeddings via Ollama (without this flag indexing is fully local and lexical, Ollama is not required)", "строить векторные эмбеддинги через Ollama (без флага индексация полностью локальная, лексическая, Ollama не требуется)"))
	fs.StringVar(&opts.Model, "model", "bge-m3", i18n.T("embedding model in Ollama (only applies with --embed)", "модель эмбеддингов в Ollama (действует только с --embed)"))
	fs.StringVar(&opts.Ext, "ext", "", i18n.T("comma-separated list of file extensions", "список расширений файлов через запятую"))
	fs.StringVar(&opts.Exclude, "exclude", "", i18n.T("comma-separated list of exclusion glob patterns", "список glob-шаблонов исключений через запятую"))
	fs.BoolVar(&opts.NoGitignore, "no-gitignore", false, i18n.T("ignore .gitignore", "не учитывать .gitignore"))
	fs.BoolVar(&opts.Hidden, "hidden", false, i18n.T("index hidden files and directories (.git and .senso stay excluded; secrets such as .env are not included)", "индексировать скрытые файлы и каталоги (.git и .senso остаются исключёнными; секреты вида .env не включаются)"))
	fs.StringVar(&opts.IncludeHidden, "include-hidden", "", i18n.T("comma-separated glob patterns of hidden or secret paths to index, for example '.github/**,.agents/**'", "список glob-шаблонов скрытых или секретных путей для индексации через запятую, например '.github/**,.agents/**'"))
	fs.BoolVar(&opts.Noisy, "noisy", false, i18n.T("index machine-generated files too: lock files, minified bundles, source maps, SVG", "индексировать и машинно-генерируемые файлы: lock-файлы, минифицированные бандлы, source maps, SVG"))
	fs.StringVar(&opts.IncludeNoisy, "include-noisy", "", i18n.T("comma-separated glob patterns of machine-generated files to index, for example 'poetry.lock,icons/**.svg'", "список glob-шаблонов машинно-генерируемых файлов для индексации через запятую, например 'poetry.lock,icons/**.svg'"))
	fs.StringVar(&opts.NoisyPatterns, "noisy-patterns", "", i18n.T("comma-separated glob patterns that replace the built-in list of machine-generated files", "список glob-шаблонов через запятую, заменяющий встроенный список машинно-генерируемых файлов"))
	fs.IntVar(&opts.ChunkSize, "chunk-size", 1200, i18n.T("chunk size in runes", "размер чанка в рунах"))
	fs.IntVar(&opts.Overlap, "overlap", 150, i18n.T("chunk overlap in runes", "перекрытие чанков в рунах"))
	fs.StringVar(&opts.QueryPrefix, "query-prefix", "", i18n.T("prefix for search queries (only applies with --embed)", "префикс для поисковых запросов (действует только с --embed)"))
	fs.StringVar(&opts.DocPrefix, "doc-prefix", "", i18n.T("prefix for documents during indexing (only applies with --embed)", "префикс для документов при индексации (действует только с --embed)"))
	fs.IntVar(&opts.MaxFileSize, "max-file-size", 10, i18n.T("maximum file size in MB", "максимальный размер файла в МБ"))
	fs.IntVar(&opts.Concurrency, "concurrency", 4, i18n.T("number of parallel embedding workers (only applies with --embed)", "число параллельных обработчиков эмбеддинга (действует только с --embed)"))
	fs.BoolVar(&opts.Prune, "prune", true, i18n.T("remove files missing from disk from the index", "удалять из индекса файлы, отсутствующие на диске"))
	fs.StringVar(&opts.Ollama, "ollama", defaultOllama, i18n.T("Ollama server address (only applies with --embed)", "адрес сервера Ollama (действует только с --embed)"))
	fs.BoolVar(&opts.Quiet, "quiet", false, i18n.T("do not print progress", "не выводить прогресс"))
	return fs
}

// parseIndexArgs разбирает аргументы командной строки подкоманды index.
func parseIndexArgs(args []string) (indexOptions, error) {
	var opts indexOptions

	fs := indexFlagSet(&opts)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), commandHelpText(indexUsageLine(), indexSummary(), fs))
	}

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
		return indexOptions{}, usagef(i18n.T("index: expected at most one positional argument (path), got %d", "index: ожидается не более одного позиционного аргумента (путь), получено %d"), len(rest))
	}

	if opts.ChunkSize <= 0 {
		return indexOptions{}, usagef("%s", i18n.T("--chunk-size must be greater than 0", "--chunk-size должен быть больше 0"))
	}
	if opts.Overlap < 0 {
		return indexOptions{}, usagef("%s", i18n.T("--overlap cannot be negative", "--overlap не может быть отрицательным"))
	}
	if opts.Overlap >= opts.ChunkSize {
		return indexOptions{}, usagef("%s", i18n.T("--overlap must be less than --chunk-size", "--overlap должен быть меньше --chunk-size"))
	}
	if opts.Concurrency <= 0 {
		return indexOptions{}, usagef("%s", i18n.T("--concurrency must be greater than 0", "--concurrency должен быть больше 0"))
	}
	if opts.MaxFileSize <= 0 {
		return indexOptions{}, usagef("%s", i18n.T("--max-file-size must be greater than 0", "--max-file-size должен быть больше 0"))
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
