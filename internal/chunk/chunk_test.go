package chunk

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestSplitKeepsParagraphsWhenTheyFit(t *testing.T) {
	text := "Первый абзац.\n\nВторой абзац.\n\nТретий абзац."
	got := Split(text, 100, 0)
	if len(got) != 1 {
		t.Fatalf("ожидался один фрагмент, получено %d: %q", len(got), got)
	}
	if got[0] != text {
		t.Errorf("текст изменён: %q", got[0])
	}
}

func TestSplitRespectsSizeInRunes(t *testing.T) {
	// Каждый абзац - 10 рун кириллицы, то есть 20 байт.
	para := strings.Repeat("я", 10)
	text := strings.Repeat(para+"\n\n", 10)

	const size = 25
	got := Split(text, size, 0)
	if len(got) < 2 {
		t.Fatalf("текст не разбит: %d фрагментов", len(got))
	}
	for i, c := range got {
		if n := utf8.RuneCountInString(c); n > size {
			t.Errorf("фрагмент %d длиной %d рун превышает лимит %d", i, n, size)
		}
	}
}

func TestSplitLongParagraphFallsBackToLines(t *testing.T) {
	line := strings.Repeat("a", 30)
	text := strings.Join([]string{line, line, line, line}, "\n")

	got := Split(text, 70, 0)
	if len(got) < 2 {
		t.Fatalf("длинный абзац не разбит: %d", len(got))
	}
	// Рез прошёл по границам строк, значит обрывков строк быть не должно.
	for i, c := range got {
		for _, l := range strings.Split(c, "\n") {
			if l != "" && len(l) != 30 {
				t.Errorf("фрагмент %d содержит обрезанную строку длиной %d", i, len(l))
			}
		}
	}
}

func TestSplitHardCutsUnbreakableLine(t *testing.T) {
	// Минифицированный файл: ни абзацев, ни переводов строки.
	text := strings.Repeat("x", 250)
	const size = 100

	got := Split(text, size, 0)
	if len(got) != 3 {
		t.Fatalf("ожидалось 3 фрагмента, получено %d", len(got))
	}
	if strings.Join(got, "") != text {
		t.Error("жёсткий рез потерял или продублировал данные")
	}
}

func TestSplitDoesNotBreakRunes(t *testing.T) {
	text := strings.Repeat("привет мир ", 60)
	for _, c := range Split(text, 40, 10) {
		if !utf8.ValidString(c) {
			t.Fatalf("фрагмент содержит разорванную руну: %q", c)
		}
	}
}

func TestSplitOverlapCarriesContext(t *testing.T) {
	text := strings.Repeat("слово ", 100)
	const size, overlap = 50, 10

	got := Split(text, size, overlap)
	if len(got) < 2 {
		t.Fatalf("нужно минимум два фрагмента, получено %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if utf8.RuneCountInString(got[i]) > size+overlap {
			t.Errorf("фрагмент %d длиннее size+overlap", i)
		}
	}
	// Хвост первого фрагмента обязан присутствовать во втором.
	prev := []rune(strings.TrimSpace(got[0]))
	tail := string(prev[len(prev)-5:])
	if !strings.Contains(got[1], tail) {
		t.Errorf("перекрытие не перенесло контекст: %q не найдено в %q", tail, got[1])
	}
}

func TestSplitDropsBlankChunks(t *testing.T) {
	text := "Абзац.\n\n\n\n   \n\n\t\n\nЕщё абзац."
	for _, c := range Split(text, 100, 0) {
		if strings.TrimSpace(c) == "" {
			t.Error("возвращён пустой фрагмент")
		}
	}
}

func TestSplitEdgeCases(t *testing.T) {
	if got := Split("", 100, 10); len(got) != 0 {
		t.Errorf("пустой текст дал %d фрагментов", len(got))
	}
	if got := Split("   \n\n  ", 100, 10); len(got) != 0 {
		t.Errorf("пробельный текст дал %d фрагментов", len(got))
	}
	if got := Split("текст", 0, 0); got != nil {
		t.Error("нулевой размер должен давать пустой результат")
	}
	// Перекрытие не меньше размера привело бы к бесконечному повтору.
	if got := Split(strings.Repeat("a b ", 100), 20, 50); len(got) == 0 {
		t.Error("некорректное перекрытие не должно ломать разбиение")
	}
}
