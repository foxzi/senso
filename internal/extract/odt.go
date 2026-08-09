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

// openDocument разбирает разметку OpenDocument и собирает из неё текст.
//
// Символьные данные берутся только внутри абзацев text:p и заголовков
// text:h: между остальными элементами лежат переводы строк и отступы,
// которыми размечен сам XML, и в тексте документа их быть не должно.
// Внутри абзаца такого форматирования нет, поэтому всё его содержимое
// (включая вложенные text:span и ссылки) идёт в результат как есть.
//
// Отдельно разбираются элементы, которые кодируют пробельные символы:
// в ODF повторяющиеся пробелы, табуляции и переводы строк записываются
// элементами text:s, text:tab и text:line-break, а не самими символами.
func openDocument(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))

	var b strings.Builder
	inPara := 0

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
					b.WriteByte('\n')
				}
			}
		case xml.CharData:
			if inPara > 0 {
				b.Write(t)
			}
		}
	}

	return strings.TrimSpace(b.String()), nil
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
