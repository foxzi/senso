package extract

import "testing"

const epubContainer = `<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

// opfXML собирает описание книги из готовых manifest и spine.
func opfXML(manifest, spine string) string {
	return `<package xmlns="http://www.idpf.org/2007/opf" version="3.0">` +
		`<manifest>` + manifest + `</manifest><spine>` + spine + `</spine></package>`
}

// chapterXML оборачивает содержимое в разметку главы.
func chapterXML(title, body string) string {
	return `<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml"><head><title>` + title + `</title>` +
		`<style>p { color: red }</style></head><body>` + body + `</body></html>`
}

// buildEpub собирает книгу из глав, перечисленных в порядке чтения.
func buildEpub(t *testing.T, chapters [][2]string) []byte {
	t.Helper()
	files := [][2]string{{"META-INF/container.xml", epubContainer}}
	manifest, spine := "", ""
	for i, ch := range chapters {
		id := string(rune('a' + i))
		manifest += `<item id="` + id + `" href="` + ch[0] + `" media-type="application/xhtml+xml"/>`
		spine += `<itemref idref="` + id + `"/>`
		files = append(files, [2]string{"OEBPS/" + ch[0], ch[1]})
	}
	files = append(files, [2]string{"OEBPS/content.opf", opfXML(manifest, spine)})
	return buildZipFiles(t, files)
}

func TestEpub(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "абзацы разделяются переводом строки",
			body: `<p>Первый абзац.</p>
			       <p>Второй абзац.</p>`,
			want: "Первый абзац.\nВторой абзац.",
		},
		{
			name: "переносы строк внутри абзаца не рвут текст",
			body: "<p>Строка одна\n   и её продолжение</p>",
			want: "Строка одна и её продолжение",
		},
		{
			name: "заголовки и пункты списка идут отдельными строками",
			body: `<h1>Глава</h1><ul><li>первый</li><li>второй</li></ul>`,
			want: "Глава\nпервый\nвторой",
		},
		{
			name: "разметка внутри абзаца не разрывает предложение",
			body: `<p>Слово <em>важное</em> в тексте</p>`,
			want: "Слово важное в тексте",
		},
		{
			name: "ячейки таблицы разделяются табуляцией",
			body: `<table><tr><th>Ключ</th><th>Значение</th></tr><tr><td>dsn</td><td>postgres</td></tr></table>`,
			want: "Ключ\tЗначение\ndsn\tpostgres",
		},
		{
			name: "мнемоники раскрываются",
			body: `<p>a &amp; b &nbsp;&mdash; c</p>`,
			want: "a & b \u00a0— c",
		},
		{
			name: "перенос строки тегом br заканчивает строку",
			body: `<p>первая<br/>вторая</p>`,
			want: "первая\nвторая",
		},
		{
			name: "содержимое скриптов и стилей в текст не идёт",
			body: `<script>var x = 1;</script><p>текст</p><style>b { color: red }</style>`,
			want: "текст",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildEpub(t, [][2]string{{"ch1.xhtml", chapterXML("Заголовок вкладки", c.body)}})
			got, err := Text("a.epub", data)
			if err != nil {
				t.Fatalf("Text() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("Text() = %q, хотим %q", got, c.want)
			}
		})
	}
}

func TestEpubChapterOrder(t *testing.T) {
	// Порядок чтения задаёт spine, а не расположение файлов в архиве.
	files := [][2]string{
		{"META-INF/container.xml", epubContainer},
		{"OEBPS/content.opf", opfXML(
			`<item id="two" href="b.xhtml" media-type="application/xhtml+xml"/>`+
				`<item id="one" href="a.xhtml" media-type="application/xhtml+xml"/>`,
			`<itemref idref="one"/><itemref idref="two"/>`)},
		{"OEBPS/b.xhtml", chapterXML("", `<p>вторая</p>`)},
		{"OEBPS/a.xhtml", chapterXML("", `<p>первая</p>`)},
	}

	got, err := Text("a.epub", buildZipFiles(t, files))
	if err != nil {
		t.Fatalf("Text() вернул ошибку: %v", err)
	}
	want := "первая\n\nвторая"
	if got != want {
		t.Errorf("Text() = %q, хотим %q", got, want)
	}
}

func TestEpubSkipsNavigation(t *testing.T) {
	// Оглавление дублирует названия глав, и в индекс попадать не должно.
	files := [][2]string{
		{"META-INF/container.xml", epubContainer},
		{"OEBPS/content.opf", opfXML(
			`<item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>`+
				`<item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>`+
				`<item id="one" href="a.xhtml" media-type="application/xhtml+xml"/>`,
			`<itemref idref="nav"/><itemref idref="ncx"/><itemref idref="one"/>`)},
		{"OEBPS/nav.xhtml", chapterXML("", `<nav><ol><li><a href="a.xhtml">Глава первая</a></li></ol></nav>`)},
		{"OEBPS/toc.ncx", `<ncx><navMap><navPoint><navLabel><text>Глава первая</text></navLabel></navPoint></navMap></ncx>`},
		{"OEBPS/a.xhtml", chapterXML("", `<p>текст главы</p>`)},
	}

	got, err := Text("a.epub", buildZipFiles(t, files))
	if err != nil {
		t.Fatalf("Text() вернул ошибку: %v", err)
	}
	if got != "текст главы" {
		t.Errorf("Text() = %q, хотим %q", got, "текст главы")
	}
}

func TestEpubHrefWithSpaceAndAnchor(t *testing.T) {
	// Ссылка на главу может быть закодирована и указывать на якорь внутри
	// файла - имя файла в архиве от этого не меняется.
	files := [][2]string{
		{"META-INF/container.xml", epubContainer},
		{"OEBPS/content.opf", opfXML(
			`<item id="one" href="text/part%20one.xhtml#start" media-type="application/xhtml+xml"/>`,
			`<itemref idref="one"/>`)},
		{"OEBPS/text/part one.xhtml", chapterXML("", `<p>найдено</p>`)},
	}

	got, err := Text("a.epub", buildZipFiles(t, files))
	if err != nil {
		t.Fatalf("Text() вернул ошибку: %v", err)
	}
	if got != "найдено" {
		t.Errorf("Text() = %q, хотим %q", got, "найдено")
	}
}

func TestEpubMissingChapterIsSkipped(t *testing.T) {
	// Битая ссылка на главу не должна стоить нам всей книги.
	files := [][2]string{
		{"META-INF/container.xml", epubContainer},
		{"OEBPS/content.opf", opfXML(
			`<item id="gone" href="gone.xhtml" media-type="application/xhtml+xml"/>`+
				`<item id="one" href="a.xhtml" media-type="application/xhtml+xml"/>`,
			`<itemref idref="gone"/><itemref idref="one"/>`)},
		{"OEBPS/a.xhtml", chapterXML("", `<p>уцелело</p>`)},
	}

	got, err := Text("a.epub", buildZipFiles(t, files))
	if err != nil {
		t.Fatalf("Text() вернул ошибку: %v", err)
	}
	if got != "уцелело" {
		t.Errorf("Text() = %q, хотим %q", got, "уцелело")
	}
}

func TestEpubErrors(t *testing.T) {
	cases := []struct {
		name string
		data func(*testing.T) []byte
	}{
		{
			name: "файл не является архивом",
			data: func(*testing.T) []byte { return []byte("не архив") },
		},
		{
			name: "в архиве нет описания контейнера",
			data: func(t *testing.T) []byte {
				return buildZipFiles(t, [][2]string{{"OEBPS/a.xhtml", chapterXML("", "<p>текст</p>")}})
			},
		},
		{
			name: "описание книги потеряно",
			data: func(t *testing.T) []byte {
				return buildZipFiles(t, [][2]string{{"META-INF/container.xml", epubContainer}})
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := Text("a.epub", c.data(t)); err == nil {
				t.Error("Text() не вернул ошибку")
			}
		})
	}
}

func TestEpubContainerWithoutRootfile(t *testing.T) {
	data := buildZipFiles(t, [][2]string{
		{"META-INF/container.xml", `<container version="1.0"><rootfiles/></container>`},
	})
	if _, err := Text("a.epub", data); err == nil {
		t.Error("Text() не вернул ошибку на контейнере без ссылки на описание книги")
	}
}
