package extract

import (
	"archive/zip"
	"bytes"
	"testing"
)

// buildZip собирает в памяти zip-архив с единственной записью entry,
// содержащей data. Используется вместо бинарных фикстур на диске.
// buildZip собирает zip-архив из одного файла.
func buildZip(t *testing.T, entry string, data string) []byte {
	t.Helper()
	return buildZipFiles(t, [][2]string{{entry, data}})
}

// buildZipFiles собирает zip-архив из нескольких файлов, сохраняя порядок:
// разбор некоторых форматов от него зависит.
func buildZipFiles(t *testing.T, files [][2]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, f := range files {
		name, data := f[0], f[1]
		e, err := w.Create(name)
		if err != nil {
			t.Fatalf("создание записи %q: %v", name, err)
		}
		if _, err := e.Write([]byte(data)); err != nil {
			t.Fatalf("запись данных в %q: %v", name, err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("закрытие архива: %v", err)
	}
	return buf.Bytes()
}

func TestSupports(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"a.docx", true},
		{"a.odt", true},
		{"a.rtf", true},
		{"A.DOCX", true},
		{"REPORT.ODT", true},
		{"NOTES.RTF", true},
		{"a.md", false},
		{"a.doc", false},
		{"", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Supports(c.name); got != c.want {
				t.Errorf("Supports(%q) = %v, хотим %v", c.name, got, c.want)
			}
		})
	}
}

// docxNS - объявление пространства имён WordprocessingML, без которого
// декодер не сможет корректно сопоставить префикс w: элементам.
const docxNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

func TestTextDocx(t *testing.T) {
	cases := []struct {
		name    string
		xmlBody string
		want    string
	}{
		{
			name:    "несколько абзацев разделяются переводом строки",
			xmlBody: `<w:document ` + docxNS + `><w:body><w:p><w:r><w:t>First</w:t></w:r></w:p><w:p><w:r><w:t>Second</w:t></w:r></w:p></w:body></w:document>`,
			want:    "First\nSecond",
		},
		{
			name:    "tab и br внутри run дают табуляцию и перевод строки",
			xmlBody: `<w:document ` + docxNS + `><w:body><w:p><w:r><w:t>A</w:t><w:tab/><w:t>B</w:t><w:br/><w:t>C</w:t></w:r></w:p></w:body></w:document>`,
			want:    "A\tB\nC",
		},
		{
			name:    "tab внутри pPr/tabs вне run не попадает в текст",
			xmlBody: `<w:document ` + docxNS + `><w:body><w:p><w:pPr><w:tabs><w:tab w:val="left" w:pos="720"/></w:tabs></w:pPr><w:r><w:t>Text</w:t></w:r></w:p></w:body></w:document>`,
			want:    "Text",
		},
		{
			name:    "instrText и delText не попадают в текст",
			xmlBody: `<w:document ` + docxNS + `><w:body><w:p><w:r><w:instrText> HYPERLINK </w:instrText><w:t>Visible</w:t></w:r></w:p><w:p><w:r><w:delText>Deleted</w:delText><w:t>Kept</w:t></w:r></w:p></w:body></w:document>`,
			want:    "Visible\nKept",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildZip(t, "word/document.xml", c.xmlBody)
			got, err := Text("a.docx", data)
			if err != nil {
				t.Fatalf("Text() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("Text() = %q, хотим %q", got, c.want)
			}
		})
	}
}

func TestTextDocxErrors(t *testing.T) {
	t.Run("отсутствие word/document.xml в архиве", func(t *testing.T) {
		data := buildZip(t, "word/other.xml", "<x/>")
		if _, err := Text("a.docx", data); err == nil {
			t.Fatal("ожидали ошибку при отсутствии word/document.xml")
		}
	})

	t.Run("данные не являются zip-архивом", func(t *testing.T) {
		if _, err := Text("a.docx", []byte("это не zip")); err == nil {
			t.Fatal("ожидали ошибку при разборе не-zip данных")
		}
	})
}

// odtNS - объявления пространств имён office: и text:, используемых
// в разметке OpenDocument.
const odtNS = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0"`

func TestTextOdt(t *testing.T) {
	cases := []struct {
		name    string
		xmlBody string
		want    string
	}{
		{
			name:    "текст до office:body игнорируется",
			xmlBody: `<office:document-content ` + odtNS + `><office:automatic-styles>StyleTextIgnored</office:automatic-styles><office:body><office:text><text:p>Hello</text:p></office:text></office:body></office:document-content>`,
			want:    "Hello",
		},
		{
			name:    "text:h и text:p дают перевод строки в конце",
			xmlBody: `<office:document-content ` + odtNS + `><office:body><office:text><text:h>Header</text:h><text:p>Para</text:p></office:text></office:body></office:document-content>`,
			want:    "Header\nPara",
		},
		{
			name:    "text:s с text:c даёт несколько пробелов, без атрибута - один",
			xmlBody: `<office:document-content ` + odtNS + `><office:body><office:text><text:p>A<text:s text:c="3"/>B<text:s/>C</text:p></office:text></office:body></office:document-content>`,
			want:    "A   B C",
		},
		{
			name:    "text:tab и text:line-break дают табуляцию и перевод строки",
			xmlBody: `<office:document-content ` + odtNS + `><office:body><office:text><text:p>A<text:tab/>B<text:line-break/>C</text:p></office:text></office:body></office:document-content>`,
			want:    "A\tB\nC",
		},
		{
			name:    "текст внутри вложенного text:span попадает в результат",
			xmlBody: `<office:document-content ` + odtNS + `><office:body><office:text><text:p>Before <text:span>Inside</text:span> After</text:p></office:text></office:body></office:document-content>`,
			want:    "Before Inside After",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildZip(t, "content.xml", c.xmlBody)
			got, err := Text("a.odt", data)
			if err != nil {
				t.Fatalf("Text() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("Text() = %q, хотим %q", got, c.want)
			}
		})
	}
}

func TestTextUnsupportedExtension(t *testing.T) {
	if _, err := Text("a.doc", []byte("что угодно")); err == nil {
		t.Fatal("ожидали ошибку для неподдерживаемого расширения .doc")
	}
}
