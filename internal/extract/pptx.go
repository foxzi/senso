package extract

import (
	"bytes"
	"encoding/xml"
	"io"
	"strings"
)

// pptx извлекает текст презентации PowerPoint (.pptx).
//
// Каждый слайд лежит в архиве отдельным файлом ppt/slides/slideN.xml.
// Заметки докладчика (ppt/notesSlides) и шаблоны оформления в текст не
// идут: в шаблонах повторяется одна и та же служебная разметка, а заметки
// к содержимому слайда относятся косвенно.
func pptx(data []byte) (string, error) {
	z, err := zipOpen(data)
	if err != nil {
		return "", err
	}

	names := zipNames(z, func(n string) bool {
		return strings.HasPrefix(n, "ppt/slides/slide") && strings.HasSuffix(n, ".xml")
	})
	sortByNumber(names)

	var out []string
	for _, n := range names {
		part, err := zipRead(z, n)
		if err != nil {
			return "", err
		}
		s, err := slide(part)
		if err != nil {
			return "", err
		}
		if s != "" {
			out = append(out, s)
		}
	}

	// Пустая строка отделяет слайды друг от друга: иначе последняя строка
	// одного слайда склеится с заголовком следующего.
	return strings.Join(out, "\n\n"), nil
}

// slide разбирает один слайд презентации.
//
// Текст лежит в элементах a:t внутри абзацев a:p; всё остальное - описание
// фигур, положения и оформления. Таблицы раскладываются построчно: ячейки
// разделяются табуляцией, строки - переводом строки, а абзац внутри ячейки
// строку не заканчивает, иначе колонки таблицы расползлись бы по строкам.
func slide(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))

	var b strings.Builder
	inText, inCell := 0, 0

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
			case "t":
				inText++
			case "tc":
				inCell++
			case "br":
				b.WriteByte('\n')
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				if inText > 0 {
					inText--
				}
			case "p":
				if inCell > 0 {
					b.WriteByte(' ')
				} else {
					b.WriteByte('\n')
				}
			case "tc":
				if inCell > 0 {
					inCell--
					b.WriteByte('\t')
				}
			case "tr":
				b.WriteByte('\n')
			}
		case xml.CharData:
			if inText > 0 {
				b.Write(t)
			}
		}
	}

	// Пустые абзацы - обычное дело: незаполненные заполнители текста есть
	// почти на каждом слайде, и пустых строк от них было бы больше, чем
	// самого текста.
	return collapse(b.String()), nil
}
