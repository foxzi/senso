package extract

import (
	"bytes"
	"encoding/xml"
	"io"
	"sort"
	"strconv"
	"strings"
)

// xlsx извлекает текст книги Excel (.xlsx).
//
// Строковые значения ячеек хранятся не в самих листах, а в общей таблице
// xl/sharedStrings.xml: ячейка ссылается на неё индексом. Поэтому таблица
// читается первой, и только потом разбираются листы.
func xlsx(data []byte) (string, error) {
	z, err := zipOpen(data)
	if err != nil {
		return "", err
	}

	var shared []string
	if part, err := zipRead(z, "xl/sharedStrings.xml"); err == nil {
		if shared, err = sharedStrings(part); err != nil {
			return "", err
		}
	}

	names := zipNames(z, func(n string) bool {
		return strings.HasPrefix(n, "xl/worksheets/") && strings.HasSuffix(n, ".xml")
	})
	// Порядок листов задан в книге, но для поиска достаточно устойчивого
	// порядка: имена файлов идут как sheet1, sheet2, ... - сортируем их
	// с учётом числа, чтобы sheet10 не оказался перед sheet2.
	sort.Slice(names, func(i, j int) bool { return sheetLess(names[i], names[j]) })

	var b strings.Builder
	for _, n := range names {
		part, err := zipRead(z, n)
		if err != nil {
			return "", err
		}
		s, err := sheet(part, shared)
		if err != nil {
			return "", err
		}
		if s == "" {
			continue
		}
		b.WriteString(s)
		// Пустая строка отделяет листы друг от друга: иначе последняя
		// строка одного листа склеится с первой строкой следующего.
		b.WriteString("\n\n")
	}

	return tidy(b.String()), nil
}

// sharedStrings разбирает общую таблицу строк.
// Элемент si - одна строка; внутри он может быть разбит на прогоны r
// с разным форматированием, и тогда все их куски t склеиваются подряд.
func sharedStrings(data []byte) ([]string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))

	var out []string
	var cur strings.Builder
	inItem, inText := 0, 0

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "si":
				inItem++
				cur.Reset()
			case "t":
				inText++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "si":
				if inItem > 0 {
					inItem--
					out = append(out, cur.String())
				}
			case "t":
				if inText > 0 {
					inText--
				}
			}
		case xml.CharData:
			if inItem > 0 && inText > 0 {
				cur.Write(t)
			}
		}
	}

	return out, nil
}

// sheet разбирает лист книги: ячейки строки разделяются табуляцией,
// строки - переводом строки.
//
// Тип ячейки задаётся атрибутом t: "s" означает ссылку на общую таблицу
// строк по индексу из v, "inlineStr" - текст прямо в ячейке внутри is,
// остальные типы (числа, даты, результаты формул) берутся из v как есть.
func sheet(data []byte, shared []string) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))

	var b strings.Builder
	var cell strings.Builder
	inCell, inValue, inText := 0, 0, 0
	cellType := ""
	rowEmpty := true

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
			switch t.Name.Local {
			case "c":
				inCell++
				cell.Reset()
				cellType = attr(t, "t")
			case "v":
				inValue++
			case "t":
				inText++
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "c":
				if inCell > 0 {
					inCell--
					s := cell.String()
					if cellType == "s" {
						s = sharedAt(shared, s)
					}
					if s != "" {
						rowEmpty = false
					}
					b.WriteString(s)
					b.WriteByte('\t')
				}
			case "v":
				if inValue > 0 {
					inValue--
				}
			case "t":
				if inText > 0 {
					inText--
				}
			case "row":
				if rowEmpty {
					// Пустые строки листа в текст не попадают: их между
					// заполненными областями бывает очень много.
					trimRow(&b)
				} else {
					b.WriteByte('\n')
				}
				rowEmpty = true
			}
		case xml.CharData:
			if inCell > 0 && (inValue > 0 || inText > 0) {
				cell.Write(t)
			}
		}
	}

	return tidy(b.String()), nil
}

// trimRow убирает из накопленного текста хвост последней строки.
func trimRow(b *strings.Builder) {
	s := b.String()
	i := strings.LastIndexByte(s, '\n')
	b.Reset()
	b.WriteString(s[:i+1])
}

// sharedAt возвращает строку общей таблицы по индексу из ячейки.
// Некорректная ссылка трактуется как пустое значение: битый файл не должен
// ронять индексацию.
func sharedAt(shared []string, index string) string {
	n, err := strconv.Atoi(strings.TrimSpace(index))
	if err != nil || n < 0 || n >= len(shared) {
		return ""
	}
	return shared[n]
}

// attr возвращает значение атрибута элемента без учёта пространства имён.
func attr(el xml.StartElement, name string) string {
	for _, a := range el.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

// sheetLess сравнивает имена файлов листов по номеру в имени.
func sheetLess(a, b string) bool {
	na, oka := sheetNum(a)
	nb, okb := sheetNum(b)
	if oka && okb && na != nb {
		return na < nb
	}
	return a < b
}

// sheetNum достаёт номер листа из имени вида xl/worksheets/sheet12.xml.
func sheetNum(name string) (int, bool) {
	s := strings.TrimSuffix(strings.TrimPrefix(name, "xl/worksheets/sheet"), ".xml")
	n, err := strconv.Atoi(s)
	return n, err == nil
}
