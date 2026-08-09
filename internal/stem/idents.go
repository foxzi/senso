package stem

import (
	"strings"
	"unicode"
)

// Idents достаёт из текста составные идентификаторы и раскладывает их так,
// чтобы найти их можно было и по частям, и целиком. Для каждого
// идентификатора, состоящего больше чем из одного слова (ReplaceFile,
// replace_file, replace-file), в результат попадают основы всех его слов и
// основа слитной формы: "replac file replacefil".
//
// Простые токены из одного слова здесь пропускаются - они и так лежат в
// колонке body, дублировать их в ids незачем.
func Idents(s string) string {
	var out []string
	for _, run := range identRuns(s) {
		words := splitIdent(run)
		if len(words) < 2 {
			continue
		}
		for _, w := range words {
			out = append(out, stemToken(w))
		}
		out = append(out, stemToken(strings.Join(words, "")))
	}
	return strings.Join(out, " ")
}

// Path готовит путь к файлу для поиска: сегменты пути, имя и расширение
// становятся обычными токенами, а составные имена дополнительно
// раскладываются на слова, как идентификаторы.
func Path(p string) string {
	tokens := Text(p)
	idents := Idents(p)
	if idents == "" {
		return tokens
	}
	return tokens + " " + idents
}

// identRuns разбивает текст на непрерывные последовательности символов,
// из которых состоят идентификаторы и имена файлов: буквы, цифры и
// внутренние разделители "_" и "-". Любой другой символ завершает
// последовательность.
func identRuns(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != '-'
	})
}

// splitIdent раскладывает идентификатор на слова в нижнем регистре.
// Разделителями служат "_", "-" и переход регистра: "ReplaceFile" и
// "replace_file" дают одинаковые слова, а "HTTPServer" - "http" и "server",
// потому что последняя заглавная буква цепочки начинает новое слово.
// Цифры остаются приклеенными к предыдущему слову ("utf8" - одно слово).
func splitIdent(s string) []string {
	var words []string
	var buf []rune

	flush := func() {
		if len(buf) > 0 {
			words = append(words, Fold(string(buf)))
			buf = nil
		}
	}

	runes := []rune(s)
	for i, r := range runes {
		switch {
		case r == '_' || r == '-':
			flush()
		case unicode.IsUpper(r) && len(buf) > 0 && startsWord(runes, i):
			flush()
			buf = append(buf, r)
		default:
			buf = append(buf, r)
		}
	}
	flush()
	return words
}

// startsWord сообщает, начинает ли заглавная буква на позиции i новое слово.
// Это так, если предыдущий символ - строчная буква или цифра (parseURL,
// utf8Decode), либо если предыдущий символ заглавный, а следующий -
// строчный, то есть заглавная буква ушла из аббревиатуры в новое слово
// (HTTPServer).
func startsWord(runes []rune, i int) bool {
	prev := runes[i-1]
	if !unicode.IsUpper(prev) {
		return true
	}
	return i+1 < len(runes) && unicode.IsLower(runes[i+1])
}
