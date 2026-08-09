package extract

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

// docx извлекает текст основной части документа Word (.docx).
// Колонтитулы, сноски и примечания лежат в отдельных частях архива
// и намеренно не читаются.
func docx(data []byte) (string, error) {
	part, err := zipEntry(data, "word/document.xml")
	if err != nil {
		return "", err
	}
	return wordML(part)
}

// wordML разбирает разметку WordprocessingML и собирает из неё текст.
//
// Значимы только элементы прогона текста: w:t даёт сам текст, w:tab и w:br -
// табуляцию и перевод строки. Конец абзаца w:p и конец строки таблицы w:tr
// дают перевод строки. Всё остальное (свойства, стили, поля, правки) -
// разметка, которая в текст не попадает.
//
// Имена элементов сравниваются без учёта пространства имён: в реальных
// файлах один и тот же элемент встречается с разными префиксами, а Local
// у них совпадает.
func wordML(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))

	var b strings.Builder
	// inRun и inText - глубина вложенности: w:tab и w:br значимы только
	// внутри прогона (в свойствах абзаца w:tab задаёт позицию табуляции
	// и текстом не является), а символьные данные - только внутри w:t.
	inRun, inText := 0, 0

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
			case "r":
				inRun++
			case "t":
				inText++
			case "tab":
				if inRun > 0 {
					b.WriteByte('\t')
				}
			case "br", "cr":
				if inRun > 0 {
					b.WriteByte('\n')
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "r":
				if inRun > 0 {
					inRun--
				}
			case "t":
				if inText > 0 {
					inText--
				}
			case "p", "tr":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText > 0 {
				b.Write(t)
			}
		}
	}

	return strings.TrimSpace(b.String()), nil
}
