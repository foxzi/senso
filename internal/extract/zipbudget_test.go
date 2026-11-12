package extract

import (
	"errors"
	"strings"
	"testing"
)

// Бюджет списывается по мере чтения: файл, занявший остаток ровно, читается,
// следующий байт уже нет.
func TestZipReadSpendsBudget(t *testing.T) {
	z, err := zipOpen(buildZipFiles(t, [][2]string{{"a", "12345"}, {"b", "678"}}))
	if err != nil {
		t.Fatalf("zipOpen: %v", err)
	}
	z.left = 8

	for _, name := range []string{"a", "b"} {
		if _, err := zipRead(z, name); err != nil {
			t.Fatalf("zipRead(%q): %v", name, err)
		}
	}
	if z.left != 0 {
		t.Errorf("остаток бюджета = %d, хотим 0", z.left)
	}
	if _, err := zipRead(z, "b"); !errors.Is(err, errUnpackBudget) {
		t.Errorf("чтение сверх бюджета: ошибка %v, хотим errUnpackBudget", err)
	}
}

func TestZipReadMissingEntry(t *testing.T) {
	z, err := zipOpen(buildZip(t, "a", "1"))
	if err != nil {
		t.Fatalf("zipOpen: %v", err)
	}
	if _, err := zipRead(z, "b"); !errors.Is(err, errNoEntry) {
		t.Errorf("ошибка %v, хотим errNoEntry", err)
	}
}

// Архив в несколько килобайт, раздувающийся в десятки мегабайт, отклоняется
// во всех zip-форматах, а не распаковывается в память целиком.
func TestTextRejectsZipBomb(t *testing.T) {
	bomb := strings.Repeat(" ", unpackFloor+1)
	half := bomb[:unpackFloor/2+1]

	cases := []struct {
		name  string
		files [][2]string
	}{
		{"a.docx", [][2]string{{"word/document.xml", bomb}}},
		{"a.odt", [][2]string{{"content.xml", bomb}}},
		{"a.xlsx", [][2]string{{"xl/sharedStrings.xml", bomb}}},
		// Бюджет общий на архив: каждый слайд по отдельности в него влезает.
		{"a.pptx", [][2]string{{"ppt/slides/slide1.xml", half}, {"ppt/slides/slide2.xml", half}}},
		{"a.epub", [][2]string{
			{"META-INF/container.xml", epubContainer},
			{"OEBPS/content.opf", opfXML(
				`<item id="a" href="a.xhtml" media-type="application/xhtml+xml"/>`,
				`<itemref idref="a"/>`)},
			{"OEBPS/a.xhtml", bomb},
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildZipFiles(t, c.files)
			if len(data) > 1<<20 {
				t.Fatalf("архив занимает %d байт, тест рассчитан на маленький", len(data))
			}
			if _, err := Text(c.name, data); !errors.Is(err, errUnpackBudget) {
				t.Errorf("ошибка %v, хотим errUnpackBudget", err)
			}
		})
	}
}

// Хорошо сжимаемый, но обычный по размеру документ бюджет не задевает.
func TestTextAllowsCompressibleDocument(t *testing.T) {
	body := strings.Repeat(`<w:p><w:r><w:t>строка</w:t></w:r></w:p>`, 100000)
	doc := `<w:document ` + docxNS + `><w:body>` + body + `</w:body></w:document>`

	got, err := Text("a.docx", buildZip(t, "word/document.xml", doc))
	if err != nil {
		t.Fatalf("Text: %v", err)
	}
	if n := strings.Count(got, "строка"); n != 100000 {
		t.Errorf("абзацев в тексте: %d, хотим 100000", n)
	}
}
