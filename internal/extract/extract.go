// Package extract извлекает простой текст из офисных документов.
//
// Поддерживаются форматы, разбираемые средствами стандартной библиотеки:
// .docx, .odt, .ods, .odp, .xlsx, .pptx и .epub (zip-контейнер с XML внутри), .rtf
// (текстовый формат с управляющими словами), .fb2 (XML) и .ipynb (JSON). Бинарный .doc
// (Word 97-2003) не поддерживается: это OLE2-контейнер, разбор которого
// потребовал бы сторонней зависимости.
//
// Задача пакета - отдать содержимое документа как обычный текст, пригодный
// для нарезки на чанки. Форматирование, стили и метаданные отбрасываются;
// абзацы разделяются переводом строки.
package extract

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Supports сообщает, умеет ли пакет извлекать текст из файла с таким именем.
// Решение принимается только по расширению, содержимое не читается.
func Supports(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx", ".odt", ".ods", ".odp", ".rtf", ".fb2", ".ipynb", ".xlsx", ".pptx", ".epub":
		return true
	}
	return false
}

// Text извлекает простой текст из документа name с содержимым data.
// Расширение имени определяет разбор; для неподдерживаемых расширений
// возвращается ошибка.
func Text(name string, data []byte) (string, error) {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx":
		return docx(data)
	case ".odt":
		return odt(data)
	case ".ods":
		return ods(data)
	case ".odp":
		return odp(data)
	case ".rtf":
		return rtf(data)
	case ".fb2":
		return fb2(data)
	case ".ipynb":
		return ipynb(data)
	case ".xlsx":
		return xlsx(data)
	case ".pptx":
		return pptx(data)
	case ".epub":
		return epub(data)
	}
	return "", fmt.Errorf("extract: unsupported format %q", filepath.Ext(name))
}

// zipEntry возвращает содержимое файла name из zip-архива data.
func zipEntry(data []byte, name string) ([]byte, error) {
	z, err := zipOpen(data)
	if err != nil {
		return nil, err
	}
	return zipRead(z, name)
}

// Бюджет распаковки архива: не больше unpackRatio байт на каждый байт
// архива и не меньше unpackFloor. Настоящие документы сжимаются в разы
// слабее, а deflate позволяет раздуть данные примерно в тысячу раз: без
// бюджета документ в несколько мегабайт занял бы гигабайты памяти.
const (
	unpackRatio = 50
	unpackFloor = 32 << 20
)

var (
	// errNoEntry - в архиве нет файла с запрошенным именем.
	errNoEntry = errors.New("not found in archive")
	// errUnpackBudget - распакованное содержимое архива превысило бюджет.
	errUnpackBudget = errors.New("extract: unpacked content is too large for the archive size")
)

// archive - zip-архив в памяти с бюджетом распаковки.
type archive struct {
	*zip.Reader
	// left - сколько байт распакованных данных ещё можно прочитать.
	left int64
}

// zipOpen открывает zip-архив, лежащий в памяти.
func zipOpen(data []byte) (*archive, error) {
	z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	return &archive{Reader: z, left: max(unpackFloor, unpackRatio*int64(len(data)))}, nil
}

// zipRead возвращает содержимое файла name из открытого архива. Прочитанное
// списывается с бюджета распаковки, в том числе при повторном чтении того
// же файла.
func zipRead(z *archive, name string) ([]byte, error) {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		// Читаем на байт больше остатка, чтобы отличить превышение от
		// файла, занявшего остаток ровно.
		out, err := io.ReadAll(io.LimitReader(rc, z.left+1))
		if err != nil {
			return nil, err
		}
		if int64(len(out)) > z.left {
			return nil, errUnpackBudget
		}
		z.left -= int64(len(out))
		return out, nil
	}
	return nil, fmt.Errorf("extract: %s %w", name, errNoEntry)
}

// zipNames возвращает имена файлов архива, для которых keep даёт true,
// в порядке их следования в архиве.
func zipNames(z *archive, keep func(string) bool) []string {
	var out []string
	for _, f := range z.File {
		if keep(f.Name) {
			out = append(out, f.Name)
		}
	}
	return out
}

// sortByNumber упорядочивает имена файлов по числу в имени: слайды и листы
// нумеруются подряд, и обычная сортировка строк поставила бы десятый перед
// вторым, перемешав документ.
func sortByNumber(names []string) {
	sort.Slice(names, func(i, j int) bool {
		ni, oki := numberIn(names[i])
		nj, okj := numberIn(names[j])
		if oki && okj && ni != nj {
			return ni < nj
		}
		return names[i] < names[j]
	})
}

// numberIn достаёт число из конца имени файла: xl/worksheets/sheet12.xml
// или ppt/slides/slide3.xml.
func numberIn(name string) (int, bool) {
	base := strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	i := len(base)
	for i > 0 && base[i-1] >= '0' && base[i-1] <= '9' {
		i--
	}
	if i == len(base) {
		return 0, false
	}
	n, err := strconv.Atoi(base[i:])
	return n, err == nil
}

// collapse приводит извлечённый текст к виду, пригодному для индексации:
// пробелы внутри строки схлопываются в один, пустые строки убираются.
// В разметке переводы строк и отступы расставлены произвольно, и без
// этого текст оказался бы рваным.
func collapse(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = squeeze(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// squeeze схлопывает подряд идущие пробелы в строке и убирает их по краям.
// Табуляция сохраняется: ею разделены ячейки таблиц, а пробелы вокруг неё
// отбрасываются.
func squeeze(s string) string {
	var b strings.Builder
	space := false
	tab := false
	for _, r := range s {
		if r == ' ' || r == '\r' {
			space = b.Len() > 0
			continue
		}
		if space && !tab && r != '\t' {
			b.WriteByte(' ')
		}
		space = false
		tab = r == '\t'
		b.WriteRune(r)
	}
	return strings.TrimRight(b.String(), " \t")
}
