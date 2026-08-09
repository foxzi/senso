package extract

import "testing"

// odpNS дополняет odtNS пространствами имён разметки рисования, которой
// Impress описывает слайды и надписи на них.
const odpNS = odtNS + ` xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0"` +
	` xmlns:presentation="urn:oasis:names:tc:opendocument:xmlns:presentation:1.0"`

// odpXML собирает документ презентации из готовых слайдов.
func odpXML(pages string) string {
	return `<office:document-content ` + odpNS + `><office:body><office:presentation>` +
		pages + `</office:presentation></office:body></office:document-content>`
}

// odpPage собирает слайд из одной надписи с готовыми абзацами.
func odpPage(paragraphs string) string {
	return `<draw:page draw:name="page1"><draw:frame presentation:class="outline">` +
		`<draw:text-box>` + paragraphs + `</draw:text-box></draw:frame></draw:page>`
}

func TestOdp(t *testing.T) {
	cases := []struct {
		name    string
		xmlBody string
		want    string
	}{
		{
			name:    "абзацы надписи идут отдельными строками",
			xmlBody: odpXML(odpPage(`<text:p>Заголовок</text:p><text:p>Подзаголовок</text:p>`)),
			want:    "Заголовок\nПодзаголовок",
		},
		{
			name: "слайды отделяются друг от друга пустой строкой",
			xmlBody: odpXML(odpPage(`<text:p>первый</text:p>`) +
				odpPage(`<text:p>второй</text:p>`)),
			want: "первый\n\nвторой",
		},
		{
			name: "текст разных надписей одного слайда остаётся вместе",
			xmlBody: odpXML(`<draw:page>` +
				`<draw:frame><draw:text-box><text:p>Заголовок</text:p></draw:text-box></draw:frame>` +
				`<draw:frame><draw:text-box><text:p>Пояснение</text:p></draw:text-box></draw:frame>` +
				`</draw:page>`),
			want: "Заголовок\nПояснение",
		},
		{
			name: "пункты списка идут отдельными строками",
			xmlBody: odpXML(odpPage(`<text:list><text:list-item><text:p>первый</text:p></text:list-item>` +
				`<text:list-item><text:p>второй</text:p></text:list-item></text:list>`)),
			want: "первый\nвторой",
		},
		{
			name: "таблица на слайде раскладывается по строкам и колонкам",
			xmlBody: odpXML(`<draw:page><draw:frame><table:table ` +
				`xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0">` +
				`<table:table-row><table:table-cell><text:p>Ключ</text:p></table:table-cell>` +
				`<table:table-cell><text:p>Значение</text:p></table:table-cell></table:table-row>` +
				`</table:table></draw:frame></draw:page>`),
			want: "Ключ\tЗначение",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildZip(t, "content.xml", c.xmlBody)
			got, err := Text("a.odp", data)
			if err != nil {
				t.Fatalf("Text() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("Text() = %q, хотим %q", got, c.want)
			}
		})
	}
}
