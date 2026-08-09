package extract

import (
	"testing"

	"golang.org/x/text/encoding/charmap"
)

func TestFB2(t *testing.T) {
	cases := []struct {
		name string
		xml  string
		want string
	}{
		{
			name: "текст description и book-title не попадает в результат",
			xml:  `<FictionBook><description><title-info><book-title>Заголовок книги</book-title></title-info></description><body><section><p>Hello</p></section></body></FictionBook>`,
			want: "Hello",
		},
		{
			name: "содержимое binary в base64 не попадает в результат",
			xml:  `<FictionBook><body><section><p>Hello</p></section></body><binary id="cover.jpg" content-type="image/jpeg">YmFzZTY0ZGF0YQ==</binary></FictionBook>`,
			want: "Hello",
		},
		{
			name: "абзацные элементы p, v, subtitle, text-author, th, td дают текст и перевод строки",
			xml: `<FictionBook><body><section>` +
				`<subtitle>Sub</subtitle>` +
				`<p>Para</p>` +
				`<poem><stanza><v>Verse</v></stanza></poem>` +
				`<epigraph><text-author>Author</text-author></epigraph>` +
				`<table><tr><th>Head</th><td>Data</td></tr></table>` +
				`</section></body></FictionBook>`,
			want: "Sub\nPara\nVerse\nAuthor\nHead\nData",
		},
		{
			name: "empty-line даёт перевод строки",
			xml:  `<FictionBook><body><section><p>First</p><empty-line/><p>Second</p></section></body></FictionBook>`,
			want: "First\n\nSecond",
		},
		{
			name: "вложенная разметка внутри абзаца не теряет текст",
			xml:  `<FictionBook><body><section><p>Hello <emphasis>world</emphasis>!</p></section></body></FictionBook>`,
			want: "Hello world!",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := fb2([]byte(c.xml))
			if err != nil {
				t.Fatalf("fb2() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("fb2(%q) = %q, хотим %q", c.xml, got, c.want)
			}
		})
	}
}

// Кодировка windows-1251, объявленная в прологе, должна распознаваться
// эвристикой text.Decode и корректно превращаться в кириллицу UTF-8.
func TestFB2Windows1251(t *testing.T) {
	const want = "Съешь ещё этих мягких французских булок да выпей же чаю"
	doc := `<?xml version="1.0" encoding="windows-1251"?>` +
		`<FictionBook><body><section><p>` + want + `</p></section></body></FictionBook>`

	data, err := charmap.Windows1251.NewEncoder().Bytes([]byte(doc))
	if err != nil {
		t.Fatalf("кодирование в windows-1251: %v", err)
	}

	got, err := fb2(data)
	if err != nil {
		t.Fatalf("fb2() вернул ошибку: %v", err)
	}
	if got != want {
		t.Errorf("fb2() = %q, хотим %q", got, want)
	}
}

func TestFB2Errors(t *testing.T) {
	t.Run("явно бинарные данные дают ошибку", func(t *testing.T) {
		data := []byte("PNG\x00\x01\x02\x03fake binary data")
		if _, err := fb2(data); err == nil {
			t.Fatal("ожидали ошибку на бинарных данных")
		}
	})
}
