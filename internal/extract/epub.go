package extract

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"net/url"
	"path"
	"strings"
)

// epub извлекает текст книги EPUB.
//
// Книга - это zip-архив, порядок глав в котором задан косвенно:
// META-INF/container.xml указывает на файл описания (OPF), в нём manifest
// сопоставляет идентификаторы с файлами, а spine перечисляет эти
// идентификаторы в порядке чтения. Главы читаются именно в этом порядке,
// иначе текст книги перемешается.
func epub(data []byte) (string, error) {
	z, err := zipOpen(data)
	if err != nil {
		return "", err
	}

	container, err := zipRead(z, "META-INF/container.xml")
	if err != nil {
		return "", err
	}
	opfPath := rootfile(container)
	if opfPath == "" {
		return "", errors.New("extract: epub container has no rootfile")
	}

	opf, err := zipRead(z, opfPath)
	if err != nil {
		return "", err
	}
	// Ссылки в manifest заданы относительно каталога с OPF.
	base := path.Dir(opfPath)

	var b strings.Builder
	for _, href := range spineHrefs(opf) {
		name := path.Join(base, cleanHref(href))
		part, err := zipRead(z, name)
		if err != nil {
			// Битая ссылка на главу не повод терять всю книгу.
			continue
		}
		s, err := xhtmlText(part)
		if err != nil || strings.TrimSpace(s) == "" {
			continue
		}
		b.WriteString(s)
		b.WriteString("\n\n")
	}

	return tidy(b.String()), nil
}

// cleanHref приводит ссылку из manifest к имени файла в архиве: убирает
// якорь и раскодирует проценты, которыми в ссылках записаны пробелы и
// не-ASCII символы.
func cleanHref(href string) string {
	if i := strings.IndexByte(href, '#'); i >= 0 {
		href = href[:i]
	}
	if s, err := url.PathUnescape(href); err == nil {
		return s
	}
	return href
}

// rootfile возвращает путь к файлу описания книги из META-INF/container.xml.
func rootfile(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if el, ok := tok.(xml.StartElement); ok && el.Name.Local == "rootfile" {
			if p := attr(el, "full-path"); p != "" {
				return p
			}
		}
	}
}

// spineHrefs возвращает пути глав книги в порядке чтения.
// Элементы manifest могут идти после spine, поэтому сначала собирается
// всё описание, и только потом идентификаторы разворачиваются в пути.
func spineHrefs(opf []byte) []string {
	dec := xml.NewDecoder(bytes.NewReader(opf))

	manifest := map[string]string{}
	var order []string

	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		el, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch el.Name.Local {
		case "item":
			// Оглавление перечисляет те же главы и в текст не идёт:
			// в epub 3 оно помечено properties="nav", в epub 2 лежит
			// отдельным типом ncx.
			if strings.Contains(attr(el, "properties"), "nav") ||
				attr(el, "media-type") == "application/x-dtbncx+xml" {
				continue
			}
			if id := attr(el, "id"); id != "" {
				manifest[id] = attr(el, "href")
			}
		case "itemref":
			if id := attr(el, "idref"); id != "" {
				order = append(order, id)
			}
		}
	}

	out := make([]string, 0, len(order))
	for _, id := range order {
		if href := manifest[id]; href != "" {
			out = append(out, href)
		}
	}
	return out
}

// blockTags - элементы, которые в тексте заканчивают строку.
var blockTags = map[string]bool{
	"p": true, "div": true, "br": true, "li": true, "tr": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true,
	"blockquote": true, "pre": true, "section": true, "article": true,
	"td": true, "th": true,
}

// xhtmlText вытаскивает текст из главы книги.
//
// Разбор идёт нестрогим XML-декодером: разметка в книгах часто не является
// строгим XML, встречаются html-мнемоники и незакрытые теги вроде <br>.
// Содержимое head (заголовок вкладки, стили, скрипты) пропускается,
// блочные элементы заканчивают строку, а переводы строк самой разметки
// считаются обычными пробелами - переносы и отступы в книге расставлены
// под ширину страницы и к структуре текста отношения не имеют.
func xhtmlText(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	dec.Strict = false
	dec.AutoClose = xml.HTMLAutoClose
	dec.Entity = xml.HTMLEntity

	var b strings.Builder
	skip := 0

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			switch {
			case name == "head" || name == "script" || name == "style":
				skip++
			case name == "br":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			switch {
			case name == "head" || name == "script" || name == "style":
				if skip > 0 {
					skip--
				}
			case name == "td" || name == "th":
				b.WriteByte('\t')
			case name != "br" && blockTags[name]:
				b.WriteByte('\n')
			}
		case xml.CharData:
			if skip == 0 {
				b.WriteString(unwrap(string(t)))
			}
		}
	}

	return collapse(b.String()), nil
}

// collapse приводит текст главы к виду, пригодному для индексации:
// пробелы внутри строки схлопываются в один, пустые строки убираются.
// В разметке переводы строк и отступы расставлены произвольно, и без
// этого текст оказался бы рваным. Табуляция сохраняется: ею разделены
// ячейки таблиц.
func collapse(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = squeeze(l); l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// unwrap заменяет пробельное форматирование разметки обычными пробелами.
// Переводы строк и отступы в книге расставлены как удобно её сборщику;
// строки и колонки текста задают только теги.
func unwrap(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		return r
	}, s)
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
