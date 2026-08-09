package extract

import "testing"

// slideXML оборачивает фигуры в разметку слайда презентации.
func slideXML(shapes string) string {
	return `<p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" ` +
		`xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main">` +
		`<p:cSld><p:spTree>` + shapes + `</p:spTree></p:cSld></p:sld>`
}

// textShape собирает фигуру с текстом из готовых абзацев.
func textShape(paragraphs string) string {
	return `<p:sp><p:txBody>` + paragraphs + `</p:txBody></p:sp>`
}

// para собирает абзац из одного прогона текста.
func para(text string) string {
	return `<a:p><a:r><a:rPr lang="ru-RU"/><a:t>` + text + `</a:t></a:r></a:p>`
}

func TestPptx(t *testing.T) {
	cases := []struct {
		name   string
		shapes string
		want   string
	}{
		{
			name:   "абзацы слайда идут отдельными строками",
			shapes: textShape(para("Заголовок") + para("Подзаголовок")),
			want:   "Заголовок\nПодзаголовок",
		},
		{
			name:   "текст разных фигур одного слайда сохраняется",
			shapes: textShape(para("Заголовок")) + textShape(para("Список")),
			want:   "Заголовок\nСписок",
		},
		{
			name: "прогоны с разным оформлением склеиваются в один абзац",
			shapes: textShape(`<a:p><a:r><a:t>Прода</a:t></a:r>` +
				`<a:r><a:rPr b="1"/><a:t>жи</a:t></a:r></a:p>`),
			want: "Продажи",
		},
		{
			name:   "пустые заполнители текста не дают пустых строк",
			shapes: textShape(`<a:p/>`+para("Текст")+`<a:p><a:endParaRPr/></a:p>`) + textShape(`<a:p/>`),
			want:   "Текст",
		},
		{
			name:   "перенос строки внутри абзаца заканчивает строку",
			shapes: textShape(`<a:p><a:r><a:t>первая</a:t></a:r><a:br/><a:r><a:t>вторая</a:t></a:r></a:p>`),
			want:   "первая\nвторая",
		},
		{
			name: "ячейки таблицы разделяются табуляцией, строки - переводом строки",
			shapes: `<p:graphicFrame><a:graphic><a:graphicData><a:tbl>` +
				`<a:tr><a:tc><a:txBody>` + para("Ключ") + `</a:txBody></a:tc>` +
				`<a:tc><a:txBody>` + para("Значение") + `</a:txBody></a:tc></a:tr>` +
				`<a:tr><a:tc><a:txBody>` + para("dsn") + `</a:txBody></a:tc>` +
				`<a:tc><a:txBody>` + para("postgres") + `</a:txBody></a:tc></a:tr>` +
				`</a:tbl></a:graphicData></a:graphic></p:graphicFrame>`,
			want: "Ключ\tЗначение\ndsn\tpostgres",
		},
		{
			name: "несколько абзацев в ячейке остаются на одной строке таблицы",
			shapes: `<a:tbl><a:tr>` +
				`<a:tc><a:txBody>` + para("Первый") + para("Второй") + `</a:txBody></a:tc>` +
				`<a:tc><a:txBody>` + para("B") + `</a:txBody></a:tc>` +
				`</a:tr></a:tbl>`,
			want: "Первый Второй\tB",
		},
		{
			name:   "описание фигур и оформления в текст не попадает",
			shapes: `<p:sp><p:nvSpPr><p:cNvPr id="2" name="Заголовок 1"/></p:nvSpPr><p:txBody>` + para("Текст") + `</p:txBody></p:sp>`,
			want:   "Текст",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildZipFiles(t, [][2]string{
				{"ppt/slides/slide1.xml", slideXML(c.shapes)},
			})
			got, err := Text("a.pptx", data)
			if err != nil {
				t.Fatalf("Text() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("Text() = %q, хотим %q", got, c.want)
			}
		})
	}
}

func TestPptxSlideOrder(t *testing.T) {
	// Слайды читаются по номеру в имени файла и отделяются пустой строкой.
	data := buildZipFiles(t, [][2]string{
		{"ppt/slides/slide10.xml", slideXML(textShape(para("десятый")))},
		{"ppt/slides/slide2.xml", slideXML(textShape(para("второй")))},
		{"ppt/slides/slide1.xml", slideXML(textShape(para("первый")))},
	})

	got, err := Text("a.pptx", data)
	if err != nil {
		t.Fatalf("Text() вернул ошибку: %v", err)
	}
	want := "первый\n\nвторой\n\nдесятый"
	if got != want {
		t.Errorf("Text() = %q, хотим %q", got, want)
	}
}

func TestPptxSkipsNotesAndLayouts(t *testing.T) {
	// Заметки докладчика и шаблоны оформления в индекс не идут.
	data := buildZipFiles(t, [][2]string{
		{"ppt/slides/slide1.xml", slideXML(textShape(para("текст слайда")))},
		{"ppt/notesSlides/notesSlide1.xml", slideXML(textShape(para("заметка докладчика")))},
		{"ppt/slideLayouts/slideLayout1.xml", slideXML(textShape(para("образец заголовка")))},
		{"ppt/slideMasters/slideMaster1.xml", slideXML(textShape(para("образец текста")))},
	})

	got, err := Text("a.pptx", data)
	if err != nil {
		t.Fatalf("Text() вернул ошибку: %v", err)
	}
	if got != "текст слайда" {
		t.Errorf("Text() = %q, хотим %q", got, "текст слайда")
	}
}

func TestPptxWithoutSlides(t *testing.T) {
	// Презентация без слайдов даёт пустой текст, а не ошибку.
	data := buildZipFiles(t, [][2]string{
		{"ppt/presentation.xml", `<p:presentation/>`},
	})

	got, err := Text("a.pptx", data)
	if err != nil {
		t.Fatalf("Text() вернул ошибку: %v", err)
	}
	if got != "" {
		t.Errorf("Text() = %q, хотим пустой текст", got)
	}
}

func TestPptxNotZip(t *testing.T) {
	if _, err := Text("a.pptx", []byte("не архив")); err == nil {
		t.Error("Text() не вернул ошибку на файле, который не является архивом")
	}
}
