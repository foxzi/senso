// Package extract извлекает простой текст из офисных документов.
//
// Поддерживаются форматы, разбираемые средствами стандартной библиотеки:
// .docx и .odt (zip-контейнер с XML внутри) и .rtf (текстовый формат
// с управляющими словами). Бинарный .doc (Word 97-2003) не поддерживается:
// это OLE2-контейнер, разбор которого потребовал бы сторонней зависимости.
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
	"strings"
)

// Supports сообщает, умеет ли пакет извлекать текст из файла с таким именем.
// Решение принимается только по расширению, содержимое не читается.
func Supports(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".docx", ".odt", ".rtf":
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
	case ".rtf":
		return rtf(data)
	}
	return "", fmt.Errorf("extract: unsupported format %q", filepath.Ext(name))
}

// zipEntry возвращает содержимое файла name из zip-архива data.
func zipEntry(data []byte, name string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
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
