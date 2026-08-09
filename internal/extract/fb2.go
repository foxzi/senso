package extract

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"strings"

	"senso/internal/text"
)

// fb2 извлекает текст книги FictionBook (.fb2).
//
// Файл целиком является XML, но объявленная в нём кодировка часто отличается
// от UTF-8 (в русскоязычных книгах это обычно windows-1251), поэтому данные
// сначала приводятся к UTF-8 общим декодером, а разбор XML идёт уже поверх
// результата.
func fb2(data []byte) (string, error) {
	s, _, ok := text.Decode(data)
	if !ok {
		return "", errors.New("extract: fb2 is not a text file")
	}
	return fictionBook([]byte(s))
}

// fictionBook разбирает разметку FictionBook и собирает из неё текст.
//
// Читаются только элементы body: description содержит метаданные, а binary -
// вложенные картинки в base64, которым в индексе делать нечего. Внутри тела
// значимы абзацные элементы (p, v, subtitle, text-author, th, td): между
// остальными элементами лежит форматирование самого XML.
func fictionBook(data []byte) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	// Кодировку уже привели к UTF-8, но объявление в прологе осталось
	// прежним: отдаём поток как есть, иначе разбор упадёт на неизвестном
	// имени кодировки.
	dec.CharsetReader = func(_ string, input io.Reader) (io.Reader, error) {
		return input, nil
	}

	var b strings.Builder
	inBody, inPara := 0, 0

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
			case "body":
				inBody++
			case "p", "v", "subtitle", "text-author", "th", "td":
				if inBody > 0 {
					inPara++
				}
			case "empty-line":
				if inBody > 0 {
					b.WriteByte('\n')
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "body":
				if inBody > 0 {
					inBody--
				}
			case "p", "v", "subtitle", "text-author", "th", "td":
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
