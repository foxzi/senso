package cli

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"senso/internal/stem"
)

// leadShare задаёт, какую долю окна сниппета отвести под текст слева от
// найденного слова. Совпадение ставится примерно на треть ширины окна:
// так видно и то, что ему предшествует, и то, что за ним следует.
const leadShare = 3

// snippetAround возвращает фрагмент text длиной до maxRunes рун,
// центрированный на первом слове, совпадающем с запросом query. Совпадение
// ищется по основам слов, поэтому "оплата" в запросе находит "оплате" в
// тексте, а префиксный терм "оплат*" - любое слово с таким началом.
//
// Если совпадение не найдено (например, слово попало в соседний чанк или
// сработал только семантический поиск), функция ведёт себя как snippet и
// возвращает начало текста.
func snippetAround(text, query string, maxRunes int) string {
	collapsed := normalizeLines(text)
	if utf8.RuneCountInString(collapsed) <= maxRunes {
		return collapsed
	}
	runes := []rune(collapsed)

	idx, ok := matchRuneIndex(collapsed, query)
	if !ok {
		return string(runes[:maxRunes]) + "..."
	}

	start := idx - maxRunes/leadShare
	if start > len(runes)-maxRunes {
		start = len(runes) - maxRunes
	}
	if start < 0 {
		start = 0
	}
	end := start + maxRunes

	out := string(runes[start:end])
	if start > 0 {
		out = "..." + out
	}
	if end < len(runes) {
		out += "..."
	}
	return out
}

// normalizeLines мягко нормализует текст перед вырезкой сниппета: убирает
// хвостовые пробелы и табуляции в каждой строке и пустые строки по краям
// всего текста, но НЕ трогает переносы строк и внутренние отступы. Это
// важно, потому что сниппет может быть фрагментом исходного кода или
// структурированного текста - он должен оставаться читаемым для человека и
// пригодным для передачи в LLM как есть, без схлопывания в одну строку.
func normalizeLines(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Trim(strings.Join(lines, "\n"), "\n")
}

// matchRuneIndex возвращает индекс руны, с которой начинается первое слово
// text, совпадающее с одним из термов query. Второе значение - признак того,
// что совпадение вообще найдено.
func matchRuneIndex(text, query string) (int, bool) {
	exact, prefixes := queryTerms(query)
	if len(exact) == 0 && len(prefixes) == 0 {
		return 0, false
	}

	for _, tok := range scanTokens(text) {
		folded := stem.Fold(tok.text)
		if exact[stem.Stem(folded)] {
			return tok.runeIndex, true
		}
		for _, p := range prefixes {
			if strings.HasPrefix(folded, p) {
				return tok.runeIndex, true
			}
		}
	}
	return 0, false
}

// queryTerms разбирает пользовательский запрос на два набора: основы обычных
// слов (сравниваются с основой слова из текста на равенство) и префиксы
// термов со звёздочкой (сравниваются как начало слова, без стемминга - ровно
// так же, как их трактует stem.Query при построении выражения для FTS5).
//
// Кавычки фраз и прочая пунктуация здесь роли не играют: для центрирования
// сниппета достаточно найти любое слово запроса.
func queryTerms(query string) (exact map[string]bool, prefixes []string) {
	exact = make(map[string]bool)
	for _, w := range wordsWithStar(stem.Fold(query)) {
		if base, ok := strings.CutSuffix(w, "*"); ok {
			if base != "" {
				prefixes = append(prefixes, base)
			}
			continue
		}
		exact[stem.Stem(w)] = true
	}
	return exact, prefixes
}

// wordsWithStar разбивает строку на слова по тем же правилам, что и
// токенизатор unicode61 (разделитель - не буква и не цифра), но сохраняет
// завершающую "*" в составе слова, так как она значима как маркер
// префиксного поиска.
func wordsWithStar(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '*'
	})
}

// token - слово текста вместе с позицией его первой руны.
type token struct {
	text      string
	runeIndex int
}

// scanTokens разбивает text на слова по правилам токенизатора unicode61,
// сохраняя для каждого слова индекс его первой руны. Индекс нужен, чтобы
// вырезать окно сниппета вокруг найденного слова.
func scanTokens(text string) []token {
	runes := []rune(text)
	var tokens []token
	start := -1
	for i, r := range runes {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
			continue
		}
		if start >= 0 {
			tokens = append(tokens, token{text: string(runes[start:i]), runeIndex: start})
			start = -1
		}
	}
	if start >= 0 {
		tokens = append(tokens, token{text: string(runes[start:]), runeIndex: start})
	}
	return tokens
}
