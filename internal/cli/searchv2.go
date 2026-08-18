package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"senso/internal/extract"
	"senso/internal/i18n"
	"senso/internal/store"
)

// Форматы вывода search. text/json/paths повторяют исторические флаги
// --json и --paths-only, json-v2 - расширенный машинный формат.
const (
	formatText   = "text"
	formatJSON   = "json"
	formatJSONV2 = "json-v2"
	formatPaths  = "paths"
)

// searchFormats - допустимые значения --format в порядке для справки.
var searchFormats = []string{formatText, formatJSON, formatJSONV2, formatPaths}

// searchSchemaV2 - версия схемы ответа формата json-v2. Живёт в ответе
// отдельным полем, чтобы агент мог отличить формат, не разбирая структуру.
const searchSchemaV2 = 2

// Режимы поиска в машинном ответе.
const (
	modeLexical  = "lexical"
	modeSemantic = "semantic"
	modeHybrid   = "hybrid"
)

// Вид показателя релевантности. Score разных режимов несопоставим между
// собой: bm25 - вес FTS5, cosine - косинусная близость, rrf - сумма
// обратных рангов при слиянии. Поэтому вид указывается рядом со score, а
// сортировать результаты можно только по rank.
const (
	scoreBM25   = "bm25"
	scoreCosine = "cosine"
	scoreRRF    = "rrf"
)

// Тип источника текста: обычный текстовый файл или документ, из которого
// текст был извлечён при индексации. Во втором случае прочитать файл сам
// агент, как правило, не может - ему нужен senso show.
const (
	sourceText      = "text"
	sourceExtracted = "extracted"
)

// Коды предупреждений формата json-v2.
const (
	warnFileMissing  = "file_missing"
	warnFileModified = "file_modified"
)

// searchResponseV2 - ответ search в формате json-v2: результаты вместе с
// контекстом запроса, который к ним привёл. Имена полей стабильны.
type searchResponseV2 struct {
	Schema   int               `json:"schema"`
	Mode     string            `json:"mode"`
	Query    string            `json:"query"`
	Filters  searchFiltersV2   `json:"filters"`
	Results  []searchResultV2  `json:"results"`
	Warnings []searchWarningV2 `json:"warnings"`
}

// searchFiltersV2 показывает, какие ограничения применены к выдаче.
// Неактивные ограничения из ответа выпадают.
type searchFiltersV2 struct {
	K           int      `json:"k"`
	Path        []string `json:"path,omitempty"`
	Ext         []string `json:"ext,omitempty"`
	Exclude     []string `json:"exclude,omitempty"`
	Root        string   `json:"root,omitempty"`
	Deduplicate bool     `json:"deduplicate,omitempty"`
	MaxPerFile  int      `json:"max_per_file,omitempty"`
}

// searchResultV2 - один результат поиска.
type searchResultV2 struct {
	// Ref - готовая ссылка для senso show.
	Ref  string `json:"ref"`
	Path string `json:"path"`
	// Chunk - номер чанка внутри файла.
	Chunk     int `json:"chunk"`
	StartLine int `json:"line_start"`
	EndLine   int `json:"line_end"`
	// Rank - место в выдаче, начиная с 1. Единственный порядок,
	// сравнимый между режимами поиска.
	Rank int `json:"rank"`
	// Score и ScoreKind - показатель релевантности и его вид.
	Score     float64 `json:"score"`
	ScoreKind string  `json:"score_kind"`
	Text      string  `json:"text"`
	// Truncated - текст чанка обрезан окном --snippet, полный доступен
	// через senso show.
	Truncated bool `json:"truncated"`
	// SourceType - "text" или "extracted", см. sourceText/sourceExtracted.
	SourceType string `json:"source_type"`
}

// searchWarningV2 - предупреждение о выдаче: код для машины, сообщение для
// человека и путь, к которому оно относится.
type searchWarningV2 struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

// searchMode возвращает код режима поиска для машинного ответа.
func searchMode(opts searchOptions) string {
	switch {
	case opts.Hybrid:
		return modeHybrid
	case opts.Semantic:
		return modeSemantic
	default:
		return modeLexical
	}
}

// searchScoreKind возвращает вид показателя релевантности режима.
func searchScoreKind(opts searchOptions) string {
	switch {
	case opts.Hybrid:
		return scoreRRF
	case opts.Semantic:
		return scoreCosine
	default:
		return scoreBM25
	}
}

// sourceTypeFor определяет, извлекался ли текст файла из документа при
// индексации: для таких файлов агент не может просто прочитать файл сам.
func sourceTypeFor(path string) string {
	if extract.Supports(path) {
		return sourceExtracted
	}
	return sourceText
}

// buildSearchResponseV2 собирает ответ формата json-v2. Предупреждения
// строятся по состоянию файлов на диске: результат мог быть найден по
// тексту, сохранённому в индексе до изменения файла.
func buildSearchResponseV2(s *store.Store, results []store.Result, opts searchOptions, filter *resultFilter) (searchResponseV2, error) {
	resp := searchResponseV2{
		Schema:   searchSchemaV2,
		Mode:     searchMode(opts),
		Query:    opts.Query,
		Filters:  buildSearchFiltersV2(opts, filter),
		Results:  make([]searchResultV2, 0, len(results)),
		Warnings: []searchWarningV2{},
	}

	scoreKind := searchScoreKind(opts)
	for i, r := range results {
		snippet := snippetAround(r.Text, opts.Query, opts.Snippet)
		resp.Results = append(resp.Results, searchResultV2{
			Ref:        fmt.Sprintf("%s#%d", r.Path, r.Seq),
			Path:       r.Path,
			Chunk:      r.Seq,
			StartLine:  r.StartLine,
			EndLine:    r.EndLine,
			Rank:       i + 1,
			Score:      r.Score,
			ScoreKind:  scoreKind,
			Text:       snippet,
			Truncated:  snippet != r.Text,
			SourceType: sourceTypeFor(r.Path),
		})
	}

	warnings, err := staleWarningsV2(s, results)
	if err != nil {
		return searchResponseV2{}, err
	}
	resp.Warnings = append(resp.Warnings, warnings...)

	return resp, nil
}

// buildSearchFiltersV2 переносит в ответ применённые ограничения выдачи.
// Списки берутся из уже разобранного фильтра, а --root - в приведённом к
// абсолютному пути виде, чтобы ответ не зависел от того, как флаг был
// записан в командной строке.
func buildSearchFiltersV2(opts searchOptions, filter *resultFilter) searchFiltersV2 {
	f := searchFiltersV2{
		K:           opts.K,
		Deduplicate: opts.Deduplicate,
		MaxPerFile:  opts.MaxPerFile,
	}
	if filter != nil {
		f.Path = filter.pathGlobs
		f.Ext = filter.exts
		f.Exclude = filter.excludeGlobs
		f.Root = filter.root
	}
	return f
}

// staleWarningsV2 проверяет файлы результатов на диске и возвращает по
// одному предупреждению на уникальный путь, в порядке первого появления
// пути в выдаче.
func staleWarningsV2(s *store.Store, results []store.Result) ([]searchWarningV2, error) {
	stale, err := staleResultPaths(s, results)
	if err != nil {
		return nil, err
	}
	var warnings []searchWarningV2
	for _, f := range stale {
		code := warnFileModified
		if f.reason == "missing" {
			code = warnFileMissing
		}
		warnings = append(warnings, searchWarningV2{
			Code:    code,
			Path:    f.path,
			Message: staleMessageV2(code),
		})
	}
	return warnings, nil
}

// staleMessageV2 - человекочитаемое пояснение к коду предупреждения.
func staleMessageV2(code string) string {
	if code == warnFileMissing {
		return i18n.T("file is no longer on disk; the text comes from the index",
			"файла больше нет на диске; текст взят из индекса")
	}
	return i18n.T("file has changed since indexing; the text comes from the index",
		"файл изменился после индексации; текст взят из индекса")
}

// printSearchJSONV2 печатает ответ формата json-v2 в stdout одним
// объектом. Пути абсолютные, чтобы ref можно было сразу передать в
// senso show из любой рабочей директории.
func printSearchJSONV2(s *store.Store, results []store.Result, opts searchOptions, filter *resultFilter) error {
	resp, err := buildSearchResponseV2(s, results, opts, filter)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(resp, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}

// searchErrorResponseV2 - ответ формата json-v2, когда поиск не удался.
// Схема та же, что у успешного ответа, поэтому агент разбирает stdout
// одинаково и смотрит на наличие поля error.
type searchErrorResponseV2 struct {
	Schema int           `json:"schema"`
	Error  searchErrorV2 `json:"error"`
}

// searchErrorV2 - машиночитаемый код ошибки и её человекочитаемое
// сообщение. Коды перечислены в errcode.go.
type searchErrorV2 struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// printSearchErrorJSONV2 печатает ошибку поиска в stdout как объект
// json-v2. Человекочитаемое сообщение об ошибке всё равно уходит в stderr
// (это делает main), поэтому stdout остаётся строго машинным.
func printSearchErrorJSONV2(err error) error {
	data, marshalErr := json.MarshalIndent(searchErrorResponseV2{
		Schema: searchSchemaV2,
		Error: searchErrorV2{
			Code:    errorCode(err),
			Message: err.Error(),
		},
	}, "", "  ")
	if marshalErr != nil {
		return marshalErr
	}
	fmt.Fprintln(os.Stdout, string(data))
	return nil
}
