// Package walk отвечает за рекурсивный обход дерева файловой системы
// с фильтрацией по расширениям, glob-исключениям и .gitignore.
//
// Walk отдаёт кандидатов по метаданным (путь, размер, время изменения).
// Проверка содержимого файла на текстовость/бинарность в этом пакете не
// делается — это задача вызывающего кода (см. пакет text).
package walk

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	ignore "github.com/sabhiram/go-gitignore"
)

// Устойчивые коды причин, по которым путь не попал в выборку. Они видны
// в диагностическом отчёте senso index, поэтому не должны меняться без
// необходимости: на них опираются вызывающие senso агенты и скрипты.
const (
	// ReasonHardExcluded - служебный каталог .git или .senso.
	ReasonHardExcluded = "hard_excluded"
	// ReasonVendor - каталог сторонних зависимостей (node_modules, vendor).
	ReasonVendor = "vendor"
	// ReasonHidden - скрытый путь без --hidden или --include-hidden.
	ReasonHidden = "hidden"
	// ReasonSecret - файл, похожий на хранилище учётных данных.
	ReasonSecret = "secret"
	// ReasonGitignore - путь исключён правилом .gitignore.
	ReasonGitignore = "gitignore"
	// ReasonExcludeGlob - путь совпал с шаблоном --exclude.
	ReasonExcludeGlob = "exclude_glob"
	// ReasonNoisy - машинно-генерируемый файл (lock, min.js, map, svg).
	ReasonNoisy = "noisy"
	// ReasonExt - расширение не входит в список --ext.
	ReasonExt = "ext"
	// ReasonSymlink - символическая ссылка: не разыменовывается, чтобы
	// не попасть в цикл.
	ReasonSymlink = "symlink"
	// ReasonEmpty - файл нулевого размера.
	ReasonEmpty = "empty"
	// ReasonTooLarge - файл больше Options.MaxFileSize.
	ReasonTooLarge = "too_large"
)

// Options задаёт параметры обхода.
type Options struct {
	Ext          []string // пусто = любые расширения
	Exclude      []string // glob-шаблоны относительно корня обхода
	MaxFileSize  int64    // 0 = без ограничения
	UseGitignore bool

	// Hidden включает скрытые файлы и каталоги (имя начинается с точки).
	// На жёстко исключённые каталоги (.git, .senso) и на файлы с
	// учётными данными не действует.
	Hidden bool

	// IncludeHidden - glob-шаблоны точечного включения скрытых путей и
	// файлов с учётными данными. Действуют даже без Hidden, но не
	// открывают жёстко исключённые каталоги.
	IncludeHidden []string

	// Noisy включает машинно-генерируемые файлы: lock-файлы,
	// минифицированные бандлы, source maps, SVG.
	Noisy bool

	// IncludeNoisy - glob-шаблоны точечного включения машинно-
	// генерируемых файлов. Действуют даже без Noisy.
	IncludeNoisy []string

	// NoisyPatterns заменяет встроенный список шумных файлов. Пустое
	// значение оставляет список по умолчанию, см. DefaultNoisyPatterns.
	NoisyPatterns []string

	// OnExclude, если задан, вызывается для каждого пути, отброшенного
	// правилами отбора. Исключённый каталог даёт один вызов: его
	// содержимое не обходится вообще, поэтому сосчитать файлы внутри него
	// нельзя, не заплатив тем самым обходом, которого правило и позволяет
	// избежать. Причина исключения файла внутри такого каталога ищется по
	// его каталогам-предкам.
	OnExclude func(Exclusion)
}

// Exclusion описывает путь, отброшенный правилами отбора.
type Exclusion struct {
	Path   string // абсолютный, filepath.Clean
	Reason string // код причины, Reason*
	IsDir  bool   // исключён весь каталог вместе с содержимым
}

// File описывает один подходящий по фильтрам файл.
type File struct {
	Path  string // абсолютный, filepath.Clean
	Rel   string // относительно корня обхода, со слэшами
	Size  int64
	MTime int64 // unix
}

// DefaultNoisyPatterns, secretFilePatterns и правила скрытых путей описаны
// в exclude.go.

// Walk обходит дерево root и вызывает fn для каждого подходящего файла.
// Ошибки чтения отдельных файлов не прерывают обход: они передаются
// в onError (если он не nil) и обход продолжается. Если fn возвращает
// ошибку, обход останавливается и эта ошибка возвращается из Walk.
func Walk(root string, opts Options, fn func(File) error, onError func(path string, err error)) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return err
	}
	rootAbs = filepath.Clean(rootAbs)

	extSet := normalizeExts(opts.Ext)

	// matchers - скомпилированные .gitignore по директориям, в которых
	// они найдены. Ключ - абсолютный чистый путь к директории.
	matchers := map[string]*ignore.GitIgnore{}

	reportErr := func(path string, err error) {
		if onError != nil {
			onError(path, err)
		}
	}

	// exclude сообщает причину отказа и возвращает skip, чтобы точки
	// отказа оставались однострочными.
	exclude := func(path, reason string, skip error) error {
		if opts.OnExclude != nil {
			opts.OnExclude(Exclusion{
				Path:   filepath.Clean(path),
				Reason: reason,
				IsDir:  skip == fs.SkipDir,
			})
		}
		return skip
	}

	walkFn := func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			reportErr(path, err)
			return nil
		}
		if d == nil {
			return nil
		}

		// Символические ссылки не разыменовываются - ни в файлы, ни в
		// директории, чтобы не попасть в цикл.
		if d.Type()&fs.ModeSymlink != 0 {
			return exclude(path, ReasonSymlink, nil)
		}

		rel, err := filepath.Rel(rootAbs, path)
		if err != nil {
			reportErr(path, err)
			return nil
		}
		rel = filepath.ToSlash(rel)
		name := d.Name()

		if d.IsDir() {
			if path != rootAbs {
				if isHardExcludedDir(name) {
					return exclude(path, ReasonHardExcluded, fs.SkipDir)
				}
				if isVendorDir(name) {
					return exclude(path, ReasonVendor, fs.SkipDir)
				}
				if isHiddenName(name) && !opts.Hidden &&
					!dirMayContainIncluded(opts.IncludeHidden, rel, name) {
					return exclude(path, ReasonHidden, fs.SkipDir)
				}
				if opts.UseGitignore && isIgnored(matchers, rootAbs, filepath.Dir(path), path) {
					return exclude(path, ReasonGitignore, fs.SkipDir)
				}
				if excludedByGlob(rootAbs, path, opts.Exclude) {
					return exclude(path, ReasonExcludeGlob, fs.SkipDir)
				}
			}
			if opts.UseGitignore {
				loadGitignore(matchers, path, reportErr)
			}
			return nil
		}

		// Обычный файл.
		if opts.UseGitignore && isIgnored(matchers, rootAbs, filepath.Dir(path), path) {
			return exclude(path, ReasonGitignore, nil)
		}
		if excludedByGlob(rootAbs, path, opts.Exclude) {
			return exclude(path, ReasonExcludeGlob, nil)
		}
		if isNoisyName(name, opts.NoisyPatterns) && !opts.Noisy &&
			!includedByGlob(opts.IncludeNoisy, rel, name) {
			return exclude(path, ReasonNoisy, nil)
		}
		// Скрытые файлы и файлы с учётными данными открываются только
		// явно: --hidden действует на скрытые, но не на секреты, а
		// точечный шаблон --include-hidden - на то и другое.
		if !includedByGlob(opts.IncludeHidden, rel, name) {
			if isHiddenName(name) && !opts.Hidden {
				return exclude(path, ReasonHidden, nil)
			}
			if isSecretName(name) {
				return exclude(path, ReasonSecret, nil)
			}
		}
		if len(extSet) > 0 && !extSet[normalizeExt(filepath.Ext(name))] {
			return exclude(path, ReasonExt, nil)
		}

		info, err := d.Info()
		if err != nil {
			reportErr(path, err)
			return nil
		}
		if info.Size() == 0 {
			return exclude(path, ReasonEmpty, nil)
		}
		if opts.MaxFileSize > 0 && info.Size() > opts.MaxFileSize {
			return exclude(path, ReasonTooLarge, nil)
		}

		f := File{
			Path:  filepath.Clean(path),
			Rel:   rel,
			Size:  info.Size(),
			MTime: info.ModTime().Unix(),
		}
		return fn(f)
	}

	return filepath.WalkDir(rootAbs, walkFn)
}

// excludedByGlob проверяет путь на совпадение с одним из пользовательских
// glob-шаблонов из Options.Exclude. Путь сравнивается относительно root.
func excludedByGlob(root, path string, patterns []string) bool {
	if len(patterns) == 0 {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	for _, p := range patterns {
		if ok, _ := doublestar.Match(p, rel); ok {
			return true
		}
	}
	return false
}

// matchesAny проверяет имя файла (без пути) на совпадение с любым из
// шаблонов.
func matchesAny(patterns []string, name string) bool {
	for _, p := range patterns {
		if ok, _ := doublestar.Match(p, name); ok {
			return true
		}
	}
	return false
}

// normalizeExt приводит расширение к единому виду: без точки, в нижнем
// регистре. Пустая строка означает отсутствие расширения у файла.
func normalizeExt(ext string) string {
	return strings.ToLower(strings.TrimPrefix(ext, "."))
}

// normalizeExts строит множество расширений из списка Options.Ext.
// Расширения могут задаваться как с точкой, так и без неё.
func normalizeExts(exts []string) map[string]bool {
	if len(exts) == 0 {
		return nil
	}
	set := make(map[string]bool, len(exts))
	for _, e := range exts {
		set[normalizeExt(e)] = true
	}
	return set
}

// loadGitignore пытается загрузить .gitignore из директории dir и, если
// он есть, сохраняет скомпилированный матчер в matchers по ключу dir.
func loadGitignore(matchers map[string]*ignore.GitIgnore, dir string, reportErr func(string, error)) {
	giPath := filepath.Join(dir, ".gitignore")
	gi, err := ignore.CompileIgnoreFile(giPath)
	if err != nil {
		if !os.IsNotExist(err) {
			reportErr(giPath, err)
		}
		return
	}
	matchers[dir] = gi
}

// isIgnored проверяет, подпадает ли path под правила .gitignore одной из
// директорий-предков (от dir до root включительно). Правило из .gitignore
// действует на поддерево своей директории, поэтому для файла проверка
// начинается с его собственной директории dir, а для директории,
// которую проверяют перед входом, - с её родителя.
func isIgnored(matchers map[string]*ignore.GitIgnore, root, dir, path string) bool {
	for {
		if gi, ok := matchers[dir]; ok {
			rel, err := filepath.Rel(dir, path)
			if err == nil && gi.MatchesPath(filepath.ToSlash(rel)) {
				return true
			}
		}
		if dir == root {
			return false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return false
		}
		dir = parent
	}
}
