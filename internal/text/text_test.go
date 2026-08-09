package text

import (
	"bytes"
	"strings"
	"testing"

	"golang.org/x/text/encoding/charmap"
)

const sampleRU = "Съешь ещё этих мягких французских булок, да выпей же чаю. " +
	"Поиск по документам должен работать одинаково хорошо на любом языке."

func TestDecodeUTF8(t *testing.T) {
	got, enc, ok := Decode([]byte(sampleRU))
	if !ok {
		t.Fatal("валидный UTF-8 не распознан как текст")
	}
	if enc != UTF8 {
		t.Errorf("кодировка = %q, ожидалась %q", enc, UTF8)
	}
	if got != sampleRU {
		t.Error("текст изменился при декодировании")
	}
}

func TestDecodeSingleByteCyrillic(t *testing.T) {
	cases := []struct {
		name string
		enc  Encoding
		cm   *charmap.Charmap
	}{
		{"CP1251", Windows1251, charmap.Windows1251},
		{"KOI8-R", KOI8R, charmap.KOI8R},
		{"CP866", CP866, charmap.CodePage866},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			raw, err := c.cm.NewEncoder().Bytes([]byte(sampleRU))
			if err != nil {
				t.Fatalf("не удалось подготовить образец: %v", err)
			}
			got, enc, ok := Decode(raw)
			if !ok {
				t.Fatal("текст в однобайтовой кодировке отброшен как бинарный")
			}
			if enc != c.enc {
				t.Errorf("кодировка = %q, ожидалась %q", enc, c.enc)
			}
			if got != sampleRU {
				t.Errorf("текст восстановлен неверно:\nполучено: %q", got)
			}
		})
	}
}

func TestDecodeRejectsBinary(t *testing.T) {
	cases := map[string][]byte{
		"нулевой байт":           {0x50, 0x4b, 0x03, 0x04, 0x00, 0x00},
		"ELF-заголовок":          {0x7f, 'E', 'L', 'F', 0x02, 0x01, 0x01, 0x00},
		"шум без нулевых байтов": bytes.Repeat([]byte{0xff, 0xfe, 0xfd, 0xfc}, 16),
	}
	for name, data := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, ok := Decode(data); ok {
				t.Error("бинарные данные приняты за текст")
			}
		})
	}
}

// Нулевой байт за границей префикса не должен влиять на решение:
// иначе результат зависел бы от размера файла.
func TestIsBinaryOnlyChecksPrefix(t *testing.T) {
	data := append([]byte(strings.Repeat("a", SniffLen)), 0x00)
	if IsBinary(data) {
		t.Error("нулевой байт за пределами префикса учтён")
	}
	if !IsBinary(append([]byte("abc"), 0x00)) {
		t.Error("нулевой байт внутри префикса пропущен")
	}
}

func TestNormalizeNFC(t *testing.T) {
	// "й" как отдельная руна против "и" + U+0306.
	composed := "й"
	decomposed := "\u0438\u0306"
	if composed == decomposed {
		t.Fatal("образцы совпали, тест бессмыслен")
	}
	if Normalize(decomposed) != composed {
		t.Error("разложенная форма не приведена к NFC")
	}
	if Normalize(composed) != composed {
		t.Error("уже нормализованная строка изменена")
	}
}

// Английский текст не содержит старших байтов, поэтому обязан
// проходить как UTF-8 без попыток подобрать кодировку.
func TestDecodeASCII(t *testing.T) {
	const s = "package main\n\nfunc main() {}\n"
	got, enc, ok := Decode([]byte(s))
	if !ok || enc != UTF8 || got != s {
		t.Errorf("ASCII обработан неверно: ok=%v enc=%q", ok, enc)
	}
}
