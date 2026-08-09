package extract

import "testing"

// sheetXML оборачивает строки листа в разметку рабочего листа Excel.
func sheetXML(rows string) string {
	return `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>` +
		rows + `</sheetData></worksheet>`
}

// sharedXML оборачивает элементы в общую таблицу строк.
func sharedXML(items string) string {
	return `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">` + items + `</sst>`
}

func TestXlsx(t *testing.T) {
	cases := []struct {
		name   string
		shared string
		rows   string
		want   string
	}{
		{
			name:   "ячейки берут текст из общей таблицы строк по индексу",
			shared: sharedXML(`<si><t>Ключ</t></si><si><t>Значение</t></si>`),
			rows: `<row><c t="s"><v>0</v></c><c t="s"><v>1</v></c></row>` +
				`<row><c t="s"><v>1</v></c><c t="s"><v>0</v></c></row>`,
			want: "Ключ\tЗначение\nЗначение\tКлюч",
		},
		{
			name:   "числа и даты берутся из значения ячейки как есть",
			shared: sharedXML(``),
			rows:   `<row><c><v>42</v></c><c><v>3.14</v></c></row>`,
			want:   "42\t3.14",
		},
		{
			name:   "строка, записанная прямо в ячейку, попадает в текст",
			shared: sharedXML(``),
			rows:   `<row><c t="inlineStr"><is><t>внутри</t></is></c></row>`,
			want:   "внутри",
		},
		{
			name:   "куски строки с разным форматированием склеиваются без разрыва",
			shared: sharedXML(`<si><r><t>Прода</t></r><r><t>жи</t></r></si>`),
			rows:   `<row><c t="s"><v>0</v></c></row>`,
			want:   "Продажи",
		},
		{
			name:   "пустые строки листа в текст не попадают",
			shared: sharedXML(`<si><t>A</t></si><si><t>B</t></si>`),
			rows: `<row><c t="s"><v>0</v></c></row>` +
				`<row/><row/>` +
				`<row><c t="s"><v>1</v></c></row>`,
			want: "A\nB",
		},
		{
			name:   "пустая ячейка сохраняет положение соседних колонок",
			shared: sharedXML(`<si><t>A</t></si><si><t>C</t></si>`),
			rows:   `<row><c t="s"><v>0</v></c><c/><c t="s"><v>1</v></c></row>`,
			want:   "A\t\tC",
		},
		{
			name:   "ссылка за пределы общей таблицы строк не роняет разбор",
			shared: sharedXML(`<si><t>A</t></si>`),
			rows:   `<row><c t="s"><v>0</v></c><c t="s"><v>99</v></c></row>`,
			want:   "A",
		},
		{
			name:   "формула отбрасывается, в текст идёт вычисленное значение",
			shared: sharedXML(``),
			rows:   `<row><c><f>SUM(A1:A2)</f><v>7</v></c></row>`,
			want:   "7",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			data := buildZipFiles(t, [][2]string{
				{"xl/sharedStrings.xml", c.shared},
				{"xl/worksheets/sheet1.xml", sheetXML(c.rows)},
			})
			got, err := Text("a.xlsx", data)
			if err != nil {
				t.Fatalf("Text() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("Text() = %q, хотим %q", got, c.want)
			}
		})
	}
}

func TestXlsxSheetOrder(t *testing.T) {
	// Листы лежат в архиве в произвольном порядке, а читаться должны по
	// номеру: sheet10 после sheet2, иначе строки книги перемешаются.
	data := buildZipFiles(t, [][2]string{
		{"xl/worksheets/sheet10.xml", sheetXML(`<row><c t="inlineStr"><is><t>десятый</t></is></c></row>`)},
		{"xl/worksheets/sheet2.xml", sheetXML(`<row><c t="inlineStr"><is><t>второй</t></is></c></row>`)},
		{"xl/worksheets/sheet1.xml", sheetXML(`<row><c t="inlineStr"><is><t>первый</t></is></c></row>`)},
	})

	got, err := Text("a.xlsx", data)
	if err != nil {
		t.Fatalf("Text() вернул ошибку: %v", err)
	}
	want := "первый\n\nвторой\n\nдесятый"
	if got != want {
		t.Errorf("Text() = %q, хотим %q", got, want)
	}
}

func TestXlsxWithoutSharedStrings(t *testing.T) {
	// Книга без строковых ячеек не содержит xl/sharedStrings.xml, и его
	// отсутствие не должно считаться ошибкой.
	data := buildZipFiles(t, [][2]string{
		{"xl/worksheets/sheet1.xml", sheetXML(`<row><c><v>1</v></c></row>`)},
	})

	got, err := Text("a.xlsx", data)
	if err != nil {
		t.Fatalf("Text() вернул ошибку: %v", err)
	}
	if got != "1" {
		t.Errorf("Text() = %q, хотим %q", got, "1")
	}
}

func TestXlsxNotZip(t *testing.T) {
	if _, err := Text("a.xlsx", []byte("не архив")); err == nil {
		t.Error("Text() не вернул ошибку на файле, который не является архивом")
	}
}
