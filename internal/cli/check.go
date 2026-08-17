package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"senso/internal/dbpath"
	"senso/internal/i18n"
	"senso/internal/store"
	"senso/internal/text"
	"senso/internal/walk"
)

// exitStale - код завершения check при устаревшем индексе. Отдельный код
// нужен агенту, чтобы отличить "индекс пора обновить" от ошибки проверки:
// 0 - индекс свежий, 3 - устарел, 1 - проверка не удалась.
const exitStale = 3

// Коды расхождений параметров индексации. В отличие от расхождений по
// файлам, они не лечатся обычным "senso index" - нужен другой набор
// флагов или пересборка индекса.
const (
	// issueNoIndex - базы данных ещё нет.
	issueNoIndex = "no_index"
	// issueModelMismatch - индекс построен другой моделью эмбеддингов.
	issueModelMismatch = "model_mismatch"
	// issueVectorsMissing - запрошен --embed, а векторов в индексе нет.
	issueVectorsMissing = "vectors_missing"
)

// checkOptions - флаги подкоманды check. Правила отбора файлов те же, что
// у index (см. addSelectionFlags): иначе проверка сравнивала бы индекс с
// другим набором файлов.
type checkOptions struct {
	indexOptions

	JSON bool
	Hash bool
}

// checkFlagSet создаёт FlagSet подкоманды check - единственное место, где
// перечислены её флаги.
func checkFlagSet(opts *checkOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("check", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	addSelectionFlags(fs, &opts.indexOptions)
	fs.BoolVar(&opts.Embed, "embed", false, i18n.T("also check compatibility with semantic search: the index must contain vectors built by the expected model", "дополнительно проверять совместимость с семантическим поиском: в индексе должны быть векторы, построенные ожидаемой моделью"))
	fs.StringVar(&opts.Model, "model", "bge-m3", i18n.T("embedding model the index is expected to be built with (only applies with --embed)", "модель эмбеддингов, которой должен быть построен индекс (действует только с --embed)"))
	fs.BoolVar(&opts.Hash, "hash", false, i18n.T("compare file contents by hash instead of mtime and size (slower, but does not report rewritten-in-place files as changed)", "сравнивать содержимое файлов по хэшу, а не по mtime и размеру (медленнее, зато файлы с тем же содержимым не считаются изменёнными)"))
	fs.BoolVar(&opts.JSON, "json", false, i18n.T("print the result as JSON", "вывести результат в формате JSON"))
	return fs
}

// parseCheckArgs разбирает аргументы командной строки подкоманды check.
func parseCheckArgs(args []string) (checkOptions, error) {
	var opts checkOptions

	fs := checkFlagSet(&opts)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), commandHelpText(checkUsageLine(), checkSummary(), fs))
	}

	if err := fs.Parse(args); err != nil {
		return checkOptions{}, finishParse(fs, err)
	}

	rest := fs.Args()
	switch len(rest) {
	case 0:
		opts.Path = "."
	case 1:
		opts.Path = rest[0]
	default:
		return checkOptions{}, usagef(i18n.T("check: expected at most one positional argument (path), got %d", "check: ожидается не более одного позиционного аргумента (путь), получено %d"), len(rest))
	}

	if opts.MaxFileSize <= 0 {
		return checkOptions{}, usagef("%s", i18n.T("--max-file-size must be greater than 0", "--max-file-size должен быть больше 0"))
	}

	return opts, nil
}

// checkIssue - расхождение параметров индексации с кодом причины.
type checkIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// checkReport - результат проверки свежести индекса. Имена полей стабильны:
// на них опираются вызывающие senso агенты.
type checkReport struct {
	// Fresh - индекс полностью соответствует состоянию файлов на диске и
	// текущим параметрам индексации.
	Fresh bool `json:"fresh"`
	// Mode - как сравнивалось содержимое: "mtime" или "hash".
	Mode string `json:"mode"`
	// Scanned - сколько файлов на диске прошло правила отбора.
	Scanned int `json:"scanned"`
	// Unchanged - файлов, совпадающих с индексом.
	Unchanged int `json:"unchanged"`
	// Changed - проиндексированных файлов, изменившихся на диске.
	Changed int `json:"changed"`
	// Missing - файлов, которые есть в индексе, но исчезли с диска.
	Missing int `json:"missing"`
	// Unindexed - файлов на диске, которых ещё нет в индексе.
	Unindexed int `json:"unindexed"`
	// Excluded - файлов, которые есть в индексе и на диске, но текущие
	// правила отбора их больше не пропускают, с разбивкой по кодам причин
	// (см. walk.Reason*).
	Excluded         int            `json:"excluded"`
	ExcludedByReason map[string]int `json:"excluded_by_reason,omitempty"`
	// Issues - расхождения параметров индексации.
	Issues []checkIssue `json:"issues"`
	// Failed - файлы, состояние которых не удалось проверить (актуально
	// только для --hash).
	Failed []reportFailure `json:"failed"`

	IndexedAt string `json:"indexed_at"`
	Model     string `json:"model"`
	Vectors   bool   `json:"vectors"`
	Database  string `json:"database"`
}

// newCheckReport создаёт отчёт с непустыми слайсами, чтобы в JSON они
// выглядели как [], а не null.
func newCheckReport() *checkReport {
	return &checkReport{Issues: []checkIssue{}, Failed: []reportFailure{}}
}

// addIssue добавляет расхождение параметров индексации.
func (r *checkReport) addIssue(code, message string) {
	r.Issues = append(r.Issues, checkIssue{Code: code, Message: message})
}

// stale сообщает, что индекс пора обновлять.
func (r *checkReport) stale() bool {
	return r.Changed > 0 || r.Missing > 0 || r.Unindexed > 0 || r.Excluded > 0 || len(r.Issues) > 0
}

// RunCheck реализует подкоманду "check": сравнивает индекс с состоянием
// файлов на диске, ничего не меняя. База открывается только для чтения,
// поэтому проверка не может повредить индекс.
func RunCheck(args []string) error {
	opts, err := parseCheckArgs(args)
	if err != nil {
		return err
	}

	root, err := filepath.Abs(opts.Path)
	if err != nil {
		return err
	}

	rep := newCheckReport()
	rep.Mode = "mtime"
	if opts.Hash {
		rep.Mode = "hash"
	}

	dbPath, err := dbpath.Find(opts.DB)
	if errors.Is(err, dbpath.ErrNotFound) {
		// Отсутствие базы - не ошибка проверки, а самый устаревший
		// возможный индекс: агенту нужно запустить senso index.
		return finishCheck(opts, missingIndexReport(rep, root, opts))
	}
	if err != nil {
		return err
	}
	rep.Database = dbPath

	s, err := store.OpenReadOnly(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()

	if err := s.CheckSchema(); err != nil {
		return err
	}

	if err := collectIndexState(s, opts, rep); err != nil {
		return err
	}
	if err := compareTree(root, opts, s, rep); err != nil {
		return err
	}

	rep.Fresh = !rep.stale()
	return finishCheck(opts, rep)
}

// missingIndexReport заполняет отчёт для случая, когда базы ещё нет: все
// найденные файлы считаются непроиндексированными.
func missingIndexReport(rep *checkReport, root string, opts checkOptions) *checkReport {
	rep.addIssue(issueNoIndex, i18n.T("index database not found", "база индекса не найдена"))

	candidates, err := scanFiles(root, opts.indexOptions, func(path string, walkErr error) {
		rep.addFailure(path, failWalk, walkErr)
	}, nil)
	if err == nil {
		rep.Scanned = len(candidates)
		rep.Unindexed = len(candidates)
	}
	rep.Fresh = false
	return rep
}

// addExclude учитывает проиндексированный файл, который перестал проходить
// правила отбора, вместе с причиной.
func (r *checkReport) addExclude(reason string) {
	r.Excluded++
	if r.ExcludedByReason == nil {
		r.ExcludedByReason = make(map[string]int)
	}
	r.ExcludedByReason[reason]++
}

// addFailure учитывает файл, состояние которого не удалось проверить.
func (r *checkReport) addFailure(path, code string, err error) {
	r.Failed = append(r.Failed, reportFailure{Path: path, Code: code, Message: err.Error()})
}

// collectIndexState читает из базы метаданные индекса и проверяет, что
// текущие параметры совместимы с тем, как индекс был построен.
func collectIndexState(s *store.Store, opts checkOptions, rep *checkReport) error {
	model, _, err := s.Meta()
	if err != nil {
		return err
	}
	rep.Model = model

	hasVectors, err := s.HasVectors()
	if err != nil {
		return err
	}
	rep.Vectors = hasVectors

	if indexedAt, err := s.GetMeta("indexed_at"); err == nil {
		rep.IndexedAt = indexedAt
	}

	// Модель и векторы важны только при --embed: без него поиск
	// лексический и эмбеддинги не используются вообще.
	if opts.Embed {
		if model != "" && model != opts.Model {
			rep.addIssue(issueModelMismatch, fmt.Sprintf(i18n.T("index was built with model %s, requested %s", "индекс построен моделью %s, запрошена %s"), model, opts.Model))
		}
		if !hasVectors {
			rep.addIssue(issueVectorsMissing, i18n.T("index has no vectors: it was built without --embed", "в индексе нет векторов: он построен без --embed"))
		}
	}
	return nil
}

// compareTree сравнивает файлы поддерева root на диске с состоянием индекса
// и раскладывает расхождения по категориям отчёта.
func compareTree(root string, opts checkOptions, s *store.Store, rep *checkReport) error {
	// Состояние индекса читается до обхода: зная его, можно запоминать
	// причины исключения только для проиндексированных файлов, а не для
	// всего отброшенного дерева.
	indexed, err := s.FileStates(root)
	if err != nil {
		return err
	}

	reasons := make(map[string]string)
	candidates, err := scanFiles(root, opts.indexOptions, func(path string, walkErr error) {
		rep.addFailure(path, failWalk, walkErr)
	}, func(e walk.Exclusion) {
		p := text.Normalize(e.Path)
		if _, ok := indexed[p]; ok || e.IsDir {
			reasons[p] = e.Reason
		}
	})
	if err != nil {
		return err
	}
	rep.Scanned = len(candidates)

	onDisk := make(map[string]struct{}, len(candidates))
	for _, path := range candidates {
		onDisk[path] = struct{}{}

		state, found := indexed[path]
		if !found {
			rep.Unindexed++
			continue
		}

		changed, err := fileChanged(path, state, opts)
		if err != nil {
			rep.addFailure(path, failureCode(err), err)
			continue
		}
		if changed {
			rep.Changed++
		} else {
			rep.Unchanged++
		}
	}

	// Файлы, которые есть в индексе, но не в текущей выборке: либо
	// удалены с диска, либо перестали проходить правила отбора - для
	// агента это разные ситуации.
	for path := range indexed {
		if _, ok := onDisk[path]; ok {
			continue
		}
		if _, err := os.Stat(path); err != nil {
			rep.Missing++
		} else {
			rep.addExclude(excludeReason(path, reasons))
		}
	}

	sortFailures(rep.Failed)
	return nil
}

// reasonUnknown - причина исключения, которую не удалось определить.
// Штатно не встречается: файл либо отброшен сам, либо лежит внутри
// отброшенного каталога.
const reasonUnknown = "unknown"

// excludeReason ищет причину исключения пути: сначала по самому пути, затем
// по каталогам-предкам, потому что файл внутри исключённого каталога
// отдельного вызова OnExclude не получает.
func excludeReason(path string, reasons map[string]string) string {
	for p := path; ; {
		if reason, ok := reasons[p]; ok {
			return reason
		}
		parent := filepath.Dir(p)
		if parent == p {
			return reasonUnknown
		}
		p = parent
	}
}

// fileChanged сообщает, отличается ли файл на диске от сохранённого в
// индексе состояния. Решение принимает тот же decideFile, что и индексация,
// поэтому check не может расходиться с index в оценке файла. Быстрый режим
// файлы не читает; с --hash при расхождении mtime или размера содержимое
// сверяется по хэшу, поэтому перезаписанный тем же текстом файл
// изменённым не считается.
func fileChanged(path string, state store.FileMeta, opts checkOptions) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		// Файл исчез между обходом и проверкой - считаем изменившимся,
		// индексация разберётся с ним сама.
		return true, nil
	}
	curMtime, curSize := info.ModTime().UnixNano(), info.Size()

	curHash := ""
	if opts.Hash && (curMtime != state.MTime || curSize != state.Size) {
		content, skip, err := readIndexable(path, int64(opts.MaxFileSize)*1024*1024)
		if err != nil {
			return false, err
		}
		if skip != "" {
			// Файл больше не индексируем (стал пустым, бинарным,
			// слишком большим) - для индекса это изменение.
			return true, nil
		}
		curHash = hashContent(content)
	}

	return decideFile(state.MTime, state.Size, state.Hash, curMtime, curSize, curHash) == actionReindex, nil
}

// sortFailures упорядочивает список ошибок по пути, чтобы вывод не зависел
// от порядка обхода каталогов.
func sortFailures(failures []reportFailure) {
	sort.Slice(failures, func(i, j int) bool { return failures[i].Path < failures[j].Path })
}

// finishCheck печатает отчёт и возвращает код завершения: устаревший
// индекс - это exitStale, а не ошибка.
func finishCheck(opts checkOptions, rep *checkReport) error {
	if opts.JSON {
		if err := json.NewEncoder(os.Stdout).Encode(rep); err != nil {
			return err
		}
	} else if !opts.Quiet {
		cwd, _ := os.Getwd()
		printCheckSummary(os.Stdout, rep, cwd)
	}

	if rep.Fresh {
		return nil
	}
	return &ExitError{Code: exitStale, Err: errors.New(i18n.T("index is out of date", "индекс устарел"))}
}

// printCheckSummary печатает человекочитаемый результат проверки.
func printCheckSummary(w io.Writer, rep *checkReport, cwd string) {
	if rep.Fresh {
		// Формулировка без согласования числительного: "1 файлов" в
		// русском выводе выглядело бы ошибкой.
		fmt.Fprintf(w, i18n.T("index is up to date; files checked: %d\n", "индекс актуален; проверено файлов: %d\n"), rep.Scanned)
	} else {
		fmt.Fprintf(w, i18n.T("index is out of date: %d changed, %d missing, %d unindexed, %d newly excluded\n",
			"индекс устарел: изменено %d, удалено %d, новых %d, теперь исключено %d\n"),
			rep.Changed, rep.Missing, rep.Unindexed, rep.Excluded)
	}

	if rep.Excluded > 0 {
		fmt.Fprintf(w, i18n.T("  newly excluded by rules: %s\n", "  теперь исключено правилами: %s\n"),
			countsByCode(rep.ExcludedByReason))
	}

	for _, issue := range rep.Issues {
		fmt.Fprintf(w, "  %s: %s\n", issue.Code, issue.Message)
	}

	if len(rep.Failed) > 0 {
		fmt.Fprintf(w, i18n.T("could not be checked: %d\n", "не удалось проверить: %d\n"), len(rep.Failed))
		for i, f := range rep.Failed {
			if i == maxFailuresShown {
				fmt.Fprintf(w, i18n.T("  ... and %d more\n", "  ... и ещё %d\n"), len(rep.Failed)-maxFailuresShown)
				break
			}
			fmt.Fprintf(w, "  %s: %s: %s\n", shortenPath(f.Path, cwd), f.Code, f.Message)
		}
	}

	if rep.IndexedAt != "" {
		fmt.Fprintf(w, i18n.T("last indexed: %s\n", "последняя индексация: %s\n"), rep.IndexedAt)
	}
	if rep.Database != "" {
		fmt.Fprintf(w, i18n.T("database: %s\n", "база: %s\n"), shortenPath(rep.Database, cwd))
	}
}
