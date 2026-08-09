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

// zipOpen открывает zip-архив, лежащий в памяти.
func zipOpen(data []byte) (*zip.Reader, error) {
	return zip.NewReader(bytes.NewReader(data), int64(len(data)))
}

// zipRead возвращает содержимое файла name из открытого архива.
func zipRead(z *zip.Reader, name string) ([]byte, error) {
	for _, f := range z.File {
		if f.Name != name {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return io.ReadAll(rc)
	}
	return nil, fmt.Errorf("extract: %s not found in archive", name)
}

// zipNames возвращает имена файлов архива, для которых keep даёт true,
// в порядке их следования в архиве.
func zipNames(z *zip.Reader, keep func(string) bool) []string {
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
