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
	if got[0].Text != text {
		t.Errorf("текст изменён: %q", got[0].Text)
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
		if n := utf8.RuneCountInString(c.Text); n > size {
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
		for _, l := range strings.Split(c.Text, "\n") {
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
	var joined strings.Builder
	for _, c := range got {
		joined.WriteString(c.Text)
	}
	if joined.String() != text {
		t.Error("жёсткий рез потерял или продублировал данные")
	}
}

func TestSplitDoesNotBreakRunes(t *testing.T) {
	text := strings.Repeat("привет мир ", 60)
	for _, c := range Split(text, 40, 10) {
		if !utf8.ValidString(c.Text) {
			t.Fatalf("фрагмент содержит разорванную руну: %q", c.Text)
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
		if utf8.RuneCountInString(got[i].Text) > size+overlap {
			t.Errorf("фрагмент %d длиннее size+overlap", i)
		}
	}
	// Хвост первого фрагмента обязан присутствовать во втором.
	prev := []rune(strings.TrimSpace(got[0].Text))
	tail := string(prev[len(prev)-5:])
	if !strings.Contains(got[1].Text, tail) {
		t.Errorf("перекрытие не перенесло контекст: %q не найдено в %q", tail, got[1].Text)
	}
}

func TestSplitDropsBlankChunks(t *testing.T) {
	text := "Абзац.\n\n\n\n   \n\n\t\n\nЕщё абзац."
	for _, c := range Split(text, 100, 0) {
		if strings.TrimSpace(c.Text) == "" {
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

func TestSplitLineRangesAreMonotonic(t *testing.T) {
	text := "Первый абзац.\nВторая строка.\n\nВторой абзац.\nЕщё строка.\n\nТретий абзац."
	got := Split(text, 15, 0)
	if len(got) < 2 {
		t.Fatalf("нужно несколько фрагментов, получено %d", len(got))
	}
	if got[0].StartLine != 1 {
		t.Errorf("первый фрагмент должен начинаться с 1 строки, получено %d", got[0].StartLine)
	}
	prevEnd := 0
	for i, c := range got {
		if c.EndLine < c.StartLine {
			t.Errorf("фрагмент %d: EndLine %d меньше StartLine %d", i, c.EndLine, c.StartLine)
		}
		if c.StartLine < prevEnd {
			t.Errorf("фрагмент %d: StartLine %d меньше EndLine предыдущего %d", i, c.StartLine, prevEnd)
		}
		prevEnd = c.EndLine
	}
}

func TestSplitLineRangesOverlapDoesNotSkipLines(t *testing.T) {
	text := strings.Repeat("слово ", 100)
	const size, overlap = 50, 10

	got := Split(text, size, overlap)
	if len(got) < 2 {
		t.Fatalf("нужно минимум два фрагмента, получено %d", len(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].StartLine > got[i-1].EndLine {
			t.Errorf("фрагмент %d: StartLine %d больше EndLine предыдущего %d", i, got[i].StartLine, got[i-1].EndLine)
		}
	}
}

func TestSplitLineNumbersOnFixedInput(t *testing.T) {
	// Строки пронумерованы вручную:
	// 1: aaaaaaaaaa
	// 2: bbbbbbbbbb
	// 3:
	// 4: cccccccccc
	// 5: dddddddddd
	text := "aaaaaaaaaa\nbbbbbbbbbb\n\ncccccccccc\ndddddddddd"

	got := Split(text, 10, 0)
	if len(got) != 4 {
		t.Fatalf("ожидалось 4 фрагмента, получено %d: %+v", len(got), got)
	}
	want := []Chunk{
		{Text: "aaaaaaaaaa", StartLine: 1, EndLine: 1},
		{Text: "bbbbbbbbbb", StartLine: 2, EndLine: 2},
		{Text: "cccccccccc", StartLine: 4, EndLine: 4},
		{Text: "dddddddddd", StartLine: 5, EndLine: 5},
	}
	for i, w := range want {
		if got[i].Text != w.Text || got[i].StartLine != w.StartLine || got[i].EndLine != w.EndLine {
			t.Errorf("фрагмент %d: получено %+v, ожидалось %+v", i, got[i], w)
		}
	}
}

