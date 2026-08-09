package extract

import "testing"

// odsTableNS дополняет odtNS пространством имён table:, используемым
// разметкой таблиц OpenDocument.
const odsTableNS = odtNS + ` xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0"`

func TestOds(t *testing.T) {
	cases := []struct {
		name    string
		xmlBody string
		want    string
	}{
		{
			name: "ячейки одной строки разделяются табуляцией, строки - переводом строки",
			xmlBody: `<office:document-content ` + odsTableNS + `><office:body><office:spreadsheet><table:table>` +
				`<table:table-row><table:table-cell><text:p>A1</text:p></table:table-cell><table:table-cell><text:p>B1</text:p></table:table-cell></table:table-row>` +
				`<table:table-row><table:table-cell><text:p>A2</text:p></table:table-cell><table:table-cell><text:p>B2</text:p></table:table-cell></table:table-row>` +
				`</table:table></office:spreadsheet></office:body></office:document-content>`,
			want: "A1\tB1\nA2\tB2",
		},
		{
			name: "несколько абзацев внутри одной ячейки склеиваются через пробел и не дают перевод строки",
			xmlBody: `<office:document-content ` + odsTableNS + `><office:body><office:spreadsheet><table:table>` +
				`<table:table-row><table:table-cell><text:p>First</text:p><text:p>Second</text:p></table:table-cell><table:table-cell><text:p>B</text:p></table:table-cell></table:table-row>` +
				`</table:table></office:spreadsheet></office:body></office:document-content>`,
			want: "First Second\tB",
		},
		{
			name: "в конце строки нет висящей табуляции, а в конце текста нет пустых строк",
			xmlBody: `<office:document-content ` + odsTableNS + `><office:body><office:spreadsheet><table:table>` +
				`<table:table-row><table:table-cell><text:p>Only</text:p></table:table-cell></table:table-row>` +
				`</table:table></office:spreadsheet></office:body></office:document-content>`,
			want: "Only",
		},
		{
			name: "пустая ячейка не ломает раскладку",
			xmlBody: `<office:document-content ` + odsTableNS + `><office:body><office:spreadsheet><table:table>` +
				`<table:table-row><table:table-cell><text:p>A</text:p></table:table-cell><table:table-cell/><table:table-cell><text:p>B</text:p></table:table-cell></table:table-row>` +
				`</table:table></office:spreadsheet></office:body></office:document-content>`,
			want: "A\t\tB",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildZip(t, "content.xml", c.xmlBody)
			got, err := Text("a.ods", data)
			if err != nil {
				t.Fatalf("Text() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("Text() = %q, хотим %q", got, c.want)
			}
		})
	}
}
