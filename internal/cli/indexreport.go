package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"senso/internal/i18n"
)

// Устойчивые коды причин, по которым файл не попал в индекс. Коды входят
// в машинный отчёт и не должны меняться без необходимости: на них
// опираются вызывающие senso агенты и скрипты.
const (
	// skipEmpty - файл пуст или из документа не извлеклось ни одного
	// непробельного символа.
	skipEmpty = "empty"
	// skipTooLarge - файл больше лимита --max-file-size.
	skipTooLarge = "too_large"
	// skipBinary - содержимое не распознано как текст.
	skipBinary = "binary"
	// skipVanished - файл исчез между сканированием и обработкой.
	skipVanished = "vanished"
	// skipNoSchema - схема базы ещё не создана, потому что первый же
	// файл не дал ни одного чанка (возможно только с --embed).
	skipNoSchema = "no_schema"

	// failRead - файл не удалось прочитать.
	failRead = "read_failed"
	// failWalk - каталог или .gitignore не удалось прочитать при обходе
	// дерева, поэтому часть файлов могла не попасть в индекс.
	failWalk = "walk_failed"
	// failExtract - документ не удалось разобрать (повреждён или это не
	// тот формат, на который указывает расширение).
	failExtract = "extract_failed"
)

// fileFailure - ошибка обработки одного файла с устойчивым кодом причины.
// Такие ошибки не прерывают индексацию: они собираются в отчёт, а на код
// возврата влияют только при --strict.
type fileFailure struct {
	Code string
	Err  error
}

func (f *fileFailure) Error() string { return f.Err.Error() }

func (f *fileFailure) Unwrap() error { return f.Err }

// failureCode возвращает код причины, если ошибка относится к обработке
// одного файла, и пустую строку для всех остальных ошибок - их нужно
// выносить наружу как ошибку всей команды.
func failureCode(err error) string {
	var f *fileFailure
	if errors.As(err, &f) {
		return f.Code
	}
	return ""
}

// reportFailure - запись об одном файле, который не удалось обработать.
type reportFailure struct {
	Path    string `json:"path"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// indexReport - сводка одного запуска index. Структура сериализуется в
// машинный отчёт (--report-json), поэтому имена полей стабильны.
type indexReport struct {
	// Scanned - сколько файлов дошло до обработки после фильтров обхода.
	Scanned int `json:"scanned"`
	// Indexed - новых файлов в индексе.
	Indexed int `json:"indexed"`
	// Updated - файлов, чьё содержимое изменилось и было переиндексировано.
	Updated int `json:"updated"`
	// Unchanged - файлов с неизменившимся содержимым, включая те, у
	// которых обновились только mtime и размер.
	Unchanged int `json:"unchanged"`
	// Deleted - записей, убранных из индекса при --prune.
	Deleted int `json:"deleted"`
	// Chunks - сколько чанков записано за этот запуск.
	Chunks int `json:"chunks"`
	// Skipped - сколько файлов пропущено, с разбивкой по кодам причин.
	Skipped       int            `json:"skipped"`
	SkippedByCode map[string]int `json:"skipped_by_code,omitempty"`
	// Excluded - сколько путей отброшено правилами отбора при обходе
	// дерева, с разбивкой по кодам причин (см. walk.Reason*). Исключённый
	// каталог считается одной записью: его содержимое не обходится.
	Excluded         int            `json:"excluded"`
	ExcludedByReason map[string]int `json:"excluded_by_reason,omitempty"`
	// Failed - файлы, обработка которых завершилась ошибкой.
	Failed []reportFailure `json:"failed"`
	// Interrupted - индексация прервана сигналом и не дошла до конца.
	Interrupted bool   `json:"interrupted"`
	DurationMS  int64  `json:"duration_ms"`
	Database    string `json:"database"`
	Vectors     bool   `json:"vectors"`
}

// newIndexReport создаёт пустой отчёт. Failed - непустой слайс, чтобы в
// JSON поле выглядело как [], а не null.
func newIndexReport() *indexReport {
	return &indexReport{Failed: []reportFailure{}}
}

// addSkip учитывает пропуск файла с указанным кодом причины.
func (r *indexReport) addSkip(code string) {
	r.Skipped++
	if r.SkippedByCode == nil {
		r.SkippedByCode = make(map[string]int)
	}
	r.SkippedByCode[code]++
}

// addExclude учитывает путь, отброшенный правилами отбора при обходе.
func (r *indexReport) addExclude(reason string) {
	r.Excluded++
	if r.ExcludedByReason == nil {
		r.ExcludedByReason = make(map[string]int)
	}
	r.ExcludedByReason[reason]++
}

// addFailure учитывает файл, который не удалось обработать.
func (r *indexReport) addFailure(path, code string, err error) {
	if code == "" {
		code = failRead
	}
	r.Failed = append(r.Failed, reportFailure{Path: path, Code: code, Message: err.Error()})
}

// countsByCode собирает счётчики в строку вида "code: n, code: n" с
// устойчивым порядком кодов - нужен для человекочитаемой сводки.
func countsByCode(counts map[string]int) string {
	codes := make([]string, 0, len(counts))
	for code := range counts {
		codes = append(codes, code)
	}
	sort.Strings(codes)
	parts := make([]string, 0, len(codes))
	for _, code := range codes {
		parts = append(parts, fmt.Sprintf("%s: %d", code, counts[code]))
	}
	return strings.Join(parts, ", ")
}

// printIndexSummary печатает человекочитаемую сводку в stderr: одну строку
// с основными счётчиками и, если есть, пропуски и ошибки по файлам.
func printIndexSummary(w io.Writer, r *indexReport, cwd string) {
	vectorsLabel := i18n.T("no", "нет")
	if r.Vectors {
		vectorsLabel = i18n.T("yes", "да")
	}
	dur := (time.Duration(r.DurationMS) * time.Millisecond).Round(time.Millisecond)

	fmt.Fprintf(w, i18n.T("done: %d files, %d indexed, %d updated, %d unchanged, %d deleted, %d chunks in %s, vectors: %s\n",
		"готово: %d файлов, %d новых, %d обновлено, %d без изменений, %d удалено, %d чанков за %s, векторы: %s\n"),
		r.Scanned, r.Indexed, r.Updated, r.Unchanged, r.Deleted, r.Chunks, dur, vectorsLabel)

	if r.Skipped > 0 {
		fmt.Fprintf(w, i18n.T("skipped: %d (%s)\n", "пропущено: %d (%s)\n"),
			r.Skipped, countsByCode(r.SkippedByCode))
	}

	if r.Excluded > 0 {
		fmt.Fprintf(w, i18n.T("excluded by rules: %d (%s)\n", "исключено правилами: %d (%s)\n"),
			r.Excluded, countsByCode(r.ExcludedByReason))
	}

	if len(r.Failed) > 0 {
		fmt.Fprintf(w, i18n.T("failed: %d\n", "не удалось обработать: %d\n"), len(r.Failed))
		for i, f := range r.Failed {
			if i == maxFailuresShown {
				fmt.Fprintf(w, i18n.T("  ... and %d more\n", "  ... и ещё %d\n"), len(r.Failed)-maxFailuresShown)
				break
			}
			fmt.Fprintf(w, "  %s: %s: %s\n", shortenPath(f.Path, cwd), f.Code, f.Message)
		}
	}

	fmt.Fprintf(w, i18n.T("database: %s\n", "база: %s\n"), shortenPath(r.Database, cwd))

	if r.Interrupted {
		fmt.Fprint(w, i18n.T("interrupted by a signal: the index is incomplete\n", "прервано сигналом: индекс неполный\n"))
	}
}

// maxFailuresShown ограничивает список ошибок в человекочитаемой сводке -
// полный список всегда доступен в машинном отчёте.
const maxFailuresShown = 10

// printIndexReportJSON выводит машинный отчёт одной строкой JSON.
func printIndexReportJSON(w io.Writer, r *indexReport) error {
	enc := json.NewEncoder(w)
	return enc.Encode(r)
}

// reportExitError переводит отчёт в код завершения процесса: прерывание
// сигналом важнее ошибок файлов, а сами ошибки влияют на код только в
// строгом режиме.
func reportExitError(r *indexReport, strict bool) error {
	if r.Interrupted {
		return &ExitError{Code: 130, Err: errors.New(i18n.T("interrupted by a signal", "прервано сигналом"))}
	}
	if strict && len(r.Failed) > 0 {
		return &ExitError{Code: 1, Err: fmt.Errorf(i18n.T("%d files failed to be processed", "не удалось обработать файлов: %d"), len(r.Failed))}
	}
	return nil
}
