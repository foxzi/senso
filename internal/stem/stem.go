// Package stem готовит текст для лексического (FTS5) индекса и поисковых
// запросов: токенизация как у unicode61, склейка ё/е и стемминг по Snowball
// (русский/английский, выбор языка по токену).
package stem

import (
	"strings"
	"unicode"

	"github.com/kljensen/snowball"
)

// Tokens разбивает текст на токены так же, как токенизатор unicode61 в FTS5:
// разделителем считается любой символ, не являющийся буквой или цифрой.
func Tokens(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
}

// Fold приводит текст к нижнему регистру и склеивает ё с е.
func Fold(s string) string {
	s = strings.ToLower(s)
	return strings.ReplaceAll(s, "ё", "е")
}

// Text готовит текст для записи в лексический индекс: стеммирует потокенно,
// сохраняя количество и порядок токенов, чтобы фразовый поиск не ломался.
func Text(s string) string {
	tokens := Tokens(Fold(s))
	for i, t := range tokens {
		tokens[i] = stemToken(t)
	}
	return strings.Join(tokens, " ")
}

// Query превращает пользовательский запрос в выражение для FTS5 MATCH.
func Query(s string) string {
	var parts []string
	rest := Fold(s)

	for {
		open := strings.IndexByte(rest, '"')
		if open == -1 {
			parts = append(parts, plainPart(rest)...)
			break
		}

		parts = append(parts, plainPart(rest[:open])...)
		rest = rest[open+1:]

		close := strings.IndexByte(rest, '"')
		if close == -1 {
			// Непарная кавычка: остаток трактуем как обычные токены.
			parts = append(parts, plainPart(rest)...)
			break
		}

		if phrase := phrasePart(rest[:close]); phrase != "" {
			parts = append(parts, phrase)
		}
		rest = rest[close+1:]
	}

	return strings.Join(parts, " ")
}

// plainPart разбивает текст вне кавычек на слова, каждое оборачивая в свои
// кавычки. Слово с завершающим "*" не стеммируется — это префиксный поиск,
// "*" выносится за пределы кавычек.
//
// Tokens() тут не подходит: для неё "*" тоже разделитель, а он должен
// остаться приклеенным к слову как маркер префикса.
func plainPart(s string) []string {
	words := wordsWithStar(s)
	out := make([]string, 0, len(words))
	for _, w := range words {
		if strings.HasSuffix(w, "*") {
			out = append(out, `"`+strings.TrimSuffix(w, "*")+`"*`)
			continue
		}
		out = append(out, `"`+stemToken(w)+`"`)
	}
	return out
}

// wordsWithStar разбивает строку так же, как Tokens (разделитель — не буква
// и не цифра), но сохраняет завершающий "*" в составе слова.
func wordsWithStar(s string) []string {
	var out []string
	var buf []rune
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			buf = append(buf, r)
		case r == '*' && len(buf) > 0:
			buf = append(buf, r)
			out = append(out, string(buf))
			buf = nil
		default:
			if len(buf) > 0 {
				out = append(out, string(buf))
				buf = nil
			}
		}
	}
	if len(buf) > 0 {
		out = append(out, string(buf))
	}
	return out
}

// phrasePart стеммирует токены фразы и склеивает их в одну кавычную группу.
func phrasePart(s string) string {
	tokens := Tokens(s)
	if len(tokens) == 0 {
		return ""
	}
	for i, t := range tokens {
		tokens[i] = stemToken(t)
	}
	return `"` + strings.Join(tokens, " ") + `"`
}

// stemToken стеммирует один токен, выбирая язык по наличию кириллицы: если
// хотя бы одна руна кириллическая — "russian", иначе "english". Ошибку
// стеммера игнорируем, возвращая токен без изменений.
func stemToken(t string) string {
	lang := "english"
	for _, r := range t {
		if unicode.Is(unicode.Cyrillic, r) {
			lang = "russian"
			break
		}
	}
	stemmed, err := snowball.Stem(t, lang, true)
	if err != nil {
		return t
	}
	return stemmed
}
