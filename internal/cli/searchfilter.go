package cli

import (
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"

	"senso/internal/i18n"
)

// resultFilter применяет к результатам поиска пользовательские фильтры
// --path/--ext/--exclude/--root. В отличие от walk.Options (фильтры при
// индексации), здесь фильтруются уже найденные чанки, поэтому сопоставление
// делается и с абсолютным путём чанка, и с путём относительно каждого
// известного корня базы - у search нет единственного "текущего" корня,
// с которым можно было бы однозначно построить относительный путь.
type resultFilter struct {
	pathGlobs    []string
	excludeGlobs []string
	exts         []string
	root         string   // "" - фильтр по корню выключен
	roots        []string // все корни базы, для сопоставления относительных путей
}

// newResultFilter разбирает значения флагов --path/--ext/--exclude/--root
// (path/exclude - glob-шаблоны через запятую, ext - расширения через
// запятую) и строит фильтр результатов. roots - корни, зарегистрированные
// в базе (store.Roots()): относительно них проверяются glob-шаблоны, и
// среди них должен быть путь, переданный в --root.
func newResultFilter(pathFlag, extFlag, excludeFlag, rootFlag string, roots []string) (*resultFilter, error) {
	f := &resultFilter{
		pathGlobs:    splitList(pathFlag),
		excludeGlobs: splitList(excludeFlag),
		exts:         normalizeExts(splitList(extFlag)),
		roots:        roots,
	}

	if rootFlag == "" {
		return f, nil
	}

	abs, err := filepath.Abs(rootFlag)
	if err != nil {
		return nil, usagef(i18n.T("--root: invalid path %q: %v", "--root: некорректный путь %q: %v"), rootFlag, err)
	}
	abs = filepath.Clean(abs)

	if !containsCleanPath(roots, abs) {
		known := i18n.T("no roots indexed yet", "корни ещё не проиндексированы")
		if len(roots) > 0 {
			known = strings.Join(roots, ", ")
		}
		return nil, usagef(i18n.T("--root %q is not one of the indexed roots: %s", "--root %q не входит в число проиндексированных корней: %s"), rootFlag, known)
	}
	f.root = abs

	return f, nil
}

// Active сообщает, включён ли хотя бы один фильтр. От этого зависит,
// нужно ли запрашивать у store расширенный пул кандидатов перед
// фильтрацией (см. searchPoolSize в search.go).
func (f *resultFilter) Active() bool {
	if f == nil {
		return false
	}
	return len(f.pathGlobs) > 0 || len(f.excludeGlobs) > 0 || len(f.exts) > 0 || f.root != ""
}

// Match сообщает, проходит ли результат с абсолютным путём path все
// активные фильтры. --exclude проверяется раньше --path и имеет над ним
// приоритет: путь, совпавший с исключением, отбрасывается, даже если он
// же совпадает с --path.
func (f *resultFilter) Match(path string) bool {
	if f == nil {
		return true
	}
	if f.root != "" && !isWithinRoot(f.root, path) {
		return false
	}
	if len(f.exts) > 0 && !matchesExt(path, f.exts) {
		return false
	}
	if len(f.excludeGlobs) > 0 && f.matchesAnyGlob(path, f.excludeGlobs) {
		return false
	}
	if len(f.pathGlobs) > 0 && !f.matchesAnyGlob(path, f.pathGlobs) {
		return false
	}
	return true
}

// matchesAnyGlob проверяет path на совпадение с любым из patterns.
// Сравнение делается и с абсолютным путём (со слэшами), и, для каждого
// известного корня, с путём относительно этого корня - шаблоны вроде
// "internal/**" пишутся относительно корня проекта, а не от "/".
func (f *resultFilter) matchesAnyGlob(path string, patterns []string) bool {
	if matchesAnyPattern(patterns, filepath.ToSlash(path)) {
		return true
	}
	for _, root := range f.roots {
		rel, err := filepath.Rel(root, path)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		if matchesAnyPattern(patterns, filepath.ToSlash(rel)) {
			return true
		}
	}
	return false
}

// matchesAnyPattern проверяет slashPath на совпадение с любым из glob-
// шаблонов patterns. Некорректные шаблоны просто не совпадают ни с чем -
// как и в internal/walk, недопустимый glob не должен обрушивать поиск.
func matchesAnyPattern(patterns []string, slashPath string) bool {
	for _, p := range patterns {
		if ok, _ := doublestar.Match(p, slashPath); ok {
			return true
		}
	}
	return false
}

// isWithinRoot сообщает, лежит ли path внутри root (или совпадает с ним).
// Оба пути ожидаются абсолютными и чистыми.
func isWithinRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel == "." || !strings.HasPrefix(rel, "..")
}

// containsCleanPath сообщает, есть ли среди roots путь, совпадающий
// с abs после filepath.Clean.
func containsCleanPath(roots []string, abs string) bool {
	for _, r := range roots {
		if filepath.Clean(r) == abs {
			return true
		}
	}
	return false
}
