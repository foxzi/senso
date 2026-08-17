package cli

import (
	"hash/fnv"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"senso/internal/extract"
	"senso/internal/text"
	"senso/internal/walk"
)

// hashContent считает FNV-1a 64 от содержимого файла и возвращает его
// в виде шестнадцатеричной строки в нижнем регистре.
func hashContent(b []byte) string {
	h := fnv.New64a()
	h.Write(b)
	return strconv.FormatUint(h.Sum64(), 16)
}

// fileAction - решение о том, что нужно сделать с файлом при индексации.
type fileAction int

const (
	actionSkip    fileAction = iota // ничего делать не нужно
	actionTouch                     // изменились только mtime/size, содержимое то же
	actionReindex                   // содержимое изменилось, нужны новые чанки
)

// decideFile - чистая функция без ввода-вывода, реализующая правило
// инкрементальности: решает, нужно ли переиндексировать файл, только
// обновить его метаданные или пропустить.
func decideFile(dbMtime, dbSize int64, dbHash string, curMtime, curSize int64, curHash string) fileAction {
	if dbHash == "" {
		return actionReindex
	}
	if curMtime == dbMtime && curSize == dbSize {
		return actionSkip
	}
	if curHash == dbHash {
		return actionTouch
	}
	return actionReindex
}

// applyBackfill корректирует решение decideFile для случая дозаполнения
// векторов: если backfill включён (запрошены эмбеддинги, а векторов в базе
// пока нет), actionSkip превращается в actionReindex, чтобы файл прошёл
// через эмбеддинг заново. Компромисс KISS: векторы не отслеживаются
// отдельно по каждому файлу, поэтому при дозаполнении переиндексируются все
// файлы, а не только те, для которых векторов не хватает.
func applyBackfill(action fileAction, backfill bool) fileAction {
	if backfill && action == actionSkip {
		return actionReindex
	}
	return action
}

// prefixInSubtree сообщает, лежит ли absPath внутри поддерева absRoot
// (или равен ему). Сравнение идёт по границам сегментов пути, поэтому
// "/a/bc" не считается лежащим внутри "/a/b".
func prefixInSubtree(absPath, absRoot string) bool {
	absPath = filepath.Clean(absPath)
	absRoot = filepath.Clean(absRoot)
	if absPath == absRoot {
		return true
	}
	return strings.HasPrefix(absPath, absRoot+string(filepath.Separator))
}

// buildWalkOptions собирает конфигурацию обхода дерева для walk.Walk
// из разобранных опций команды index.
func buildWalkOptions(opts indexOptions, root string) walk.Options {
	return walk.Options{
		Ext:           normalizeExts(splitList(opts.Ext)),
		Exclude:       splitList(opts.Exclude),
		MaxFileSize:   int64(opts.MaxFileSize) * 1024 * 1024,
		UseGitignore:  !opts.NoGitignore,
		Hidden:        opts.Hidden,
		IncludeHidden: splitList(opts.IncludeHidden),
		Noisy:         opts.Noisy,
		IncludeNoisy:  splitList(opts.IncludeNoisy),
		NoisyPatterns: splitList(opts.NoisyPatterns),
	}
}

// scanFiles обходит root и возвращает отсортированный список абсолютных
// путей файлов-кандидатов на индексацию с применёнными фильтрами.
// Проверка текстовости содержимого здесь не делается.
func scanFiles(root string, opts indexOptions, onError func(path string, err error), onExclude func(walk.Exclusion)) ([]string, error) {
	walkOpts := buildWalkOptions(opts, root)
	walkOpts.OnExclude = onExclude

	// Все правила исключений (скрытые пути, секреты, шум, служебные
	// каталоги) живут в пакете walk - единственном месте, где они заданы.
	var result []string
	err := walk.Walk(root, walkOpts, func(f walk.File) error {
		result = append(result, text.Normalize(f.Path))
		return nil
	}, onError)
	if err != nil {
		return nil, err
	}

	sort.Strings(result)
	return result, nil
}

// readIndexable читает файл и готовит его содержимое к индексации:
// отсеивает пустые, слишком большие и бинарные файлы, извлекает текст из
// офисных документов, перекодирует однобайтовую кириллицу и приводит текст
// к NFC. Второе значение - код причины пропуска (см. indexreport.go);
// пустая строка означает, что файл нужно индексировать. Ошибки чтения и
// разбора возвращаются как *fileFailure: они относятся к одному файлу и не
// должны прерывать индексацию.
func readIndexable(path string, maxBytes int64) ([]byte, string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", &fileFailure{Code: failRead, Err: err}
	}
	if len(data) == 0 {
		return nil, skipEmpty, nil
	}
	if maxBytes > 0 && int64(len(data)) > maxBytes {
		return nil, skipTooLarge, nil
	}

	if extract.Supports(path) {
		return extractIndexable(path, data)
	}

	s, _, ok := text.Decode(data)
	if !ok {
		return nil, skipBinary, nil
	}
	return []byte(s), "", nil
}

// extractIndexable достаёт текст из офисного документа. Повреждённый файл
// даёт ошибку с кодом extract_failed - раньше такие файлы молча исчезали
// из индекса.
func extractIndexable(path string, data []byte) ([]byte, string, error) {
	s, err := extract.Text(path, data)
	if err != nil {
		return nil, "", &fileFailure{Code: failExtract, Err: err}
	}
	if strings.TrimSpace(s) == "" {
		return nil, skipEmpty, nil
	}
	return []byte(text.Normalize(s)), "", nil
}
