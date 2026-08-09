package extract

import "testing"

func TestRTFErrors(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"пустые данные", []byte("")},
		{"нет открывающей группы", []byte("просто текст без rtf")},
		{"открывающая скобка без управляющего слова", []byte("{просто текст}")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if _, err := rtf(c.data); err == nil {
				t.Fatal("ожидали ошибку: данные не похожи на rtf")
			}
		})
	}
}

// Фрагменты RTF без версии в заголовке (их пишет, например, pandoc)
// должны разбираться так же, как полноценные документы.
func TestRTFFragmentWithoutVersion(t *testing.T) {
	got, err := rtf([]byte(`{\pard \ql \f0 Hello\par}`))
	if err != nil {
		t.Fatalf("rtf() вернул ошибку: %v", err)
	}
	if got != "Hello" {
		t.Errorf("rtf() = %q, хотим %q", got, "Hello")
	}
}

// Регрессия: счётчик символов-заменителей после \uN не должен переживать
// границу группы. Иначе \uN внутри служебной группы (например в таблице
// шрифтов) съедал первый символ следующего за ней текста.
func TestRTFPendingDoesNotLeakFromGroup(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "u внутри пропускаемой группы",
			input: `{\rtf1\ansi{\fonttbl{\f0\u1054\'3f Times;}}\u1054\'3fK}`,
			want:  "ОK",
		},
		{
			name:  "u без заменителя перед закрытием группы",
			input: `{\rtf1\ansi{\u1054}X}`,
			want:  "ОX",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := rtf([]byte(c.input))
			if err != nil {
				t.Fatalf("rtf() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("rtf(%q) = %q, хотим %q", c.input, got, c.want)
			}
		})
	}
}

func TestRTF(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "простой документ",
			input: `{\rtf1\ansi Hello World}`,
			want:  "Hello World",
		},
		{
			name:  "par даёт перевод строки",
			input: `{\rtf1\ansi Line1\par Line2}`,
			want:  "Line1\nLine2",
		},
		{
			name:  "tab даёт табуляцию",
			input: `{\rtf1\ansi Hello\tab World}`,
			want:  "Hello\tWorld",
		},
		{
			name:  "группа fonttbl выбрасывается целиком",
			input: `{\rtf1\ansi{\fonttbl{\f0 Times New Roman;}}Hello}`,
			want:  "Hello",
		},
		{
			name:  "группа *\\generator выбрасывается целиком",
			input: `{\rtf1\ansi{\*\generator Riched20}Hello}`,
			want:  "Hello",
		},
		{
			name:  "экранированные слэш и фигурные скобки дают литералы",
			input: `{\rtf1\ansi Brace\{open\}close Slash\\end}`,
			want:  "Brace{open}close Slash\\end",
		},
		{
			name:  "hh с ansicpg1251 даёт кириллицу",
			input: `{\rtf1\ansi\ansicpg1251 \'cf\'f0\'e8\'e2\'e5\'f2}`,
			want:  "Привет",
		},
		{
			name:  "u с заменителем по умолчанию (uc1) - заменитель проглатывается",
			input: `{\rtf1\ansi\u1055?}`,
			want:  "П",
		},
		{
			name:  "uc0 отменяет проглатывание заменителя",
			input: `{\rtf1\ansi\uc0\u1055 X}`,
			want:  "ПX",
		},
		{
			name:  "отрицательный параметр u сдвигается на 65536",
			input: `{\rtf1\ansi\u-3072?}`,
			want:  string(rune(-3072 + 65536)),
		},
		{
			name:  "один пробел после управляющего слова - разделитель",
			input: `{\rtf1\ansi Foo\li0 Bar}`,
			want:  "FooBar",
		},
		{
			name:  "два пробела после управляющего слова - второй остаётся текстом",
			input: `{\rtf1\ansi Foo\li0  Bar}`,
			want:  "Foo Bar",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := rtf([]byte(c.input))
			if err != nil {
				t.Fatalf("rtf() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("rtf(%q) = %q, хотим %q", c.input, got, c.want)
			}
		})
	}
}
