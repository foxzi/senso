package cli

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// TestSnippetAroundCentersOnMatch проверяет, что окно сниппета вырезается
// вокруг слова запроса, а не с начала текста.
func TestSnippetAroundCentersOnMatch(t *testing.T) {
	filler := strings.Repeat("lorem ipsum dolor ", 40)
	text := filler + "webhook payload " + filler

	got := snippetAround(text, "webhook", 60)

	if !strings.Contains(got, "webhook") {
		t.Errorf("сниппет не содержит слово запроса: %q", got)
	}
	if !strings.HasPrefix(got, "...") {
		t.Errorf("сниппет из середины текста должен начинаться с многоточия: %q", got)
	}
}

// TestSnippetAroundMatchesWordForms проверяет, что совпадение ищется по
// основам слов: запрос "оплата" должен находить форму "оплате" в тексте.
func TestSnippetAroundMatchesWordForms(t *testing.T) {
	filler := strings.Repeat("текст без совпадений ", 40)
	text := filler + "решение об оплате заказа " + filler

	got := snippetAround(text, "оплата", 60)

	if !strings.Contains(got, "оплате") {
		t.Errorf("сниппет не центрирован на словоформе: %q", got)
	}
}

// TestSnippetAroundPrefixTerm проверяет поддержку префиксного терма.
func TestSnippetAroundPrefixTerm(t *testing.T) {
	filler := strings.Repeat("прочий текст ", 40)
	text := filler + "оплатить счёт " + filler

	got := snippetAround(text, "оплат*", 60)

	if !strings.Contains(got, "оплатить") {
		t.Errorf("префиксный терм не нашёл слово: %q", got)
	}
}

// TestSnippetAroundQuotedPhrase проверяет, что кавычки фразового запроса не
// мешают найти слово в тексте.
func TestSnippetAroundQuotedPhrase(t *testing.T) {
	filler := strings.Repeat("прочий текст ", 40)
	text := filler + "оплата партов подтверждена " + filler

	got := snippetAround(text, `"оплаты партам"`, 60)

	if !strings.Contains(got, "оплата") && !strings.Contains(got, "партов") {
		t.Errorf("фразовый запрос не нашёл слово: %q", got)
	}
}

// TestSnippetAroundFallsBackToStart проверяет поведение при отсутствии
// совпадения: возвращается начало текста, как у прежней обрезки.
func TestSnippetAroundFallsBackToStart(t *testing.T) {
	text := strings.Repeat("abcdefghij ", 40)

	got := snippetAround(text, "нетутакого", 30)

	if strings.HasPrefix(got, "...") {
		t.Errorf("при отсутствии совпадения сниппет должен начинаться с начала текста: %q", got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("обрезанный сниппет должен заканчиваться многоточием: %q", got)
	}
}

// TestSnippetAroundShortTextUnchanged проверяет, что короткий текст
// возвращается целиком и без многоточий.
func TestSnippetAroundShortTextUnchanged(t *testing.T) {
	if got := snippetAround("hello world", "world", 100); got != "hello world" {
		t.Errorf("snippetAround(короткий) = %q, хотим %q", got, "hello world")
	}
}

// TestSnippetAroundRespectsLimit проверяет, что длина окна не превышает
// заданную (многоточия добавляются сверх окна и здесь не учитываются).
func TestSnippetAroundRespectsLimit(t *testing.T) {
	filler := strings.Repeat("слово ", 60)
	text := filler + "цель " + filler
	const maxRunes = 40

	got := strings.Trim(snippetAround(text, "цель", maxRunes), ".")

	if n := utf8.RuneCountInString(got); n > maxRunes {
		t.Errorf("окно сниппета = %d рун, ожидалось не более %d", n, maxRunes)
	}
}

// TestSnippetAroundPreservesNewlines проверяет, что многострочный текст
// (например, фрагмент кода) остаётся многострочным в сниппете: переносы
// строк и отступы не схлопываются в один пробел.
func TestSnippetAroundPreservesNewlines(t *testing.T) {
	text := "func handler() {\n    payload := webhook.Read()\n    return payload\n}"

	got := snippetAround(text, "webhook", 500)

	if !strings.Contains(got, "\n") {
		t.Errorf("сниппет потерял переносы строк: %q", got)
	}
	if !strings.Contains(got, "\n    payload") {
		t.Errorf("сниппет потерял отступ строки: %q", got)
	}
}

// TestNormalizeLinesTrimsTrailingWhitespaceAndOuterBlankLines проверяет, что
// normalizeLines убирает хвостовые пробелы в строках и пустые строки по
// краям текста, но сохраняет внутренние переносы и одиночные пустые строки
// между абзацами.
func TestNormalizeLinesTrimsTrailingWhitespaceAndOuterBlankLines(t *testing.T) {
	text := "\n\n  первая строка  \n\nвторая строка\t\n\n\n"

	got := normalizeLines(text)

	want := "  первая строка\n\nвторая строка"
	if got != want {
		t.Errorf("normalizeLines = %q, ожидалось %q", got, want)
	}
}

// TestSnippetAroundMatchAtEnd проверяет, что совпадение в самом конце текста
// попадает в окно и окно не выходит за границы среза.
func TestSnippetAroundMatchAtEnd(t *testing.T) {
	text := strings.Repeat("прочий текст ", 40) + "финальноеслово"

	got := snippetAround(text, "финальноеслово", 40)

	if !strings.Contains(got, "финальноеслово") {
		t.Errorf("совпадение в конце текста не попало в сниппет: %q", got)
	}
}
