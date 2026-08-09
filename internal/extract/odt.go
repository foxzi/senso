package extract

import (
	"bytes"
	"encoding/xml"
	"io"
	"strconv"
	"strings"
)

// odt извлекает текст документа OpenDocument Text (.odt).
func odt(data []byte) (string, error) {
	part, err := zipEntry(data, "content.xml")
	if err != nil {
		return "", err
	}
	return openDocument(part)
}

// ods извлекает текст электронной таблицы OpenDocument (.ods).
// Разметка та же, что у .odt, отличается только наполнением: весь текст
// лежит в ячейках таблиц.
func ods(data []byte) (string, error) {
	return odt(data)
}

// odp извлекает текст презентации OpenDocument (.odp).
// Разметка та же, что у .odt: текст слайда лежит в обычных абзацах
// внутри надписей.
func odp(data []byte) (string, error) {
	return odt(data)
}

// openDocument разбирает разметку OpenDocument и собирает из неё текст.
//
// Символьные данные берутся только внутри абзацев text:p и заголовков
// text:h: между остальными элементами лежат переводы строк и отступы,
// которыми размечен сам XML, и в тексте документа их быть не должно.
// Внутри абзаца такого форматирования нет, поэтому всё его содержимое
// (включая вложенные text:span и ссылки) идёт в результат как есть.
//
// Таблицы раскладываются построчно: ячейки одной строки разделяются
// табуляцией, строки - переводом строки. Абзац внутри ячейки строку не
// заканчивает, иначе каждая ячейка оказалась бы на своей строке и связь
// между колонками при поиске терялась бы.
//
// Отдельно разбираются элементы, которые кодируют пробельные символы:
// в ODF повторяющиеся пробелы, табуляции и переводы строк записываются
// элементами text:s, text:tab и text:line-break, а не самими символами.
func openDocument(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))

	var b strings.Builder
	inPara, inCell := 0, 0
	// Абзацы внутри одной ячейки разделяются пробелом, но ставить его
	// сразу нельзя: если абзац последний, пробел повиснет перед
	// разделителем ячеек. Поэтому разделитель откладывается до
	// следующего текста в той же ячейке.
	gap := false

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
			case "p", "h":
				inPara++
			case "table-cell":
				inCell++
			case "s":
				if inPara > 0 {
					b.WriteString(strings.Repeat(" ", spaceCount(t)))
				}
			case "tab":
				if inPara > 0 {
					b.WriteByte('\t')
				}
			case "line-break":
				if inPara > 0 {
					b.WriteByte('\n')
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p", "h":
				if inPara > 0 {
					inPara--
					if inCell == 0 {
						b.WriteByte('\n')
					} else {
						gap = true
					}
				}
			case "table-cell":
				if inCell > 0 {
					inCell--
					gap = false
					b.WriteByte('\t')
				}
			case "table-row":
				b.WriteByte('\n')
			case "page":
				// Слайды презентации отделяются друг от друга пустой
				// строкой: подряд идущий текст разных слайдов связан
				// между собой слабо.
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inPara > 0 {
				if gap {
					gap = false
					b.WriteByte(' ')
				}
				b.Write(t)
			}
		}
	}

	return tidy(b.String()), nil
}

// tidy убирает пробельные хвосты строк и пустые строки по краям текста.
// Хвосты остаются от разделителей ячеек: последняя ячейка строки таблицы
// всё равно дописывает табуляцию, а абзац внутри ячейки - пробел.
func tidy(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// spaceCount возвращает число пробелов, закодированных элементом text:s.
// Атрибут text:c необязателен и по умолчанию равен единице; некорректное
// или отрицательное значение трактуется как один пробел.
func spaceCount(el xml.StartElement) int {
	for _, a := range el.Attr {
		if a.Name.Local != "c" {
			continue
		}
		n, err := strconv.Atoi(a.Value)
		if err != nil || n < 1 {
			return 1
		}
		return n
	}
	return 1
}
