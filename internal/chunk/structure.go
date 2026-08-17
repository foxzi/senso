package chunk

import (
	"path/filepath"
	"strings"
)

// Strategy - способ выбора границ фрагментов.
type Strategy string

const (
	// Auto добавляет к текстовому разбиению структурные границы, если тип
	// файла известен: заголовки Markdown, объявления функций и классов,
	// верхнеуровневые элементы JSON и YAML.
	Auto Strategy = "auto"
	// Text - универсальное разбиение по абзацам и строкам, одинаковое для
	// всех файлов. Запасной вариант и способ вернуть прежнее поведение.
	Text Strategy = "text"
)

// ParseStrategy разбирает имя стратегии, полученное из флага командной строки.
func ParseStrategy(s string) (Strategy, bool) {
	switch Strategy(s) {
	case Auto:
		return Auto, true
	case Text:
		return Text, true
	}
	return "", false
}

// SplitFile разбивает содержимое файла path на фрагменты, учитывая его
// структуру, если стратегия это допускает и тип файла распознан.
//
// Структурные границы - только предпочтение, а не жёсткое правило: фрагмент
// никогда не выходит за size, а блок начинает новый фрагмент лишь тогда, когда
// текущий уже заполнен хотя бы наполовину. Без этого условия файл из десятков
// коротких методов дал бы десятки крошечных фрагментов, каждый из которых
// сам по себе бессмысленен.
func SplitFile(path, text string, size, overlap int, s Strategy) []Chunk {
	if s != Auto {
		return Split(text, size, overlap)
	}
	return splitWith(text, size, overlap, boundaries(path, text))
}

// boundaries возвращает возрастающие байтовые смещения начал строк, в которых
// начинается новый структурный блок. Первая строка файла в список не попадает:
// она и так начинает первый фрагмент.
func boundaries(path, text string) []int {
	detect := detector(path)
	if detect == nil {
		return nil
	}
	ls := textLines(text)
	if len(ls) < 2 {
		return nil
	}
	return detect(ls)
}

// line - строка файла без завершающего перевода и её байтовое смещение.
type line struct {
	off  int
	text string
}

// textLines разбирает текст на строки, сохраняя смещение каждой из них.
func textLines(text string) []line {
	var ls []line
	off := 0
	for {
		i := strings.IndexByte(text[off:], '\n')
		if i < 0 {
			ls = append(ls, line{off, text[off:]})
			return ls
		}
		ls = append(ls, line{off, text[off : off+i]})
		off += i + 1
		if off == len(text) {
			return ls
		}
	}
}

// detector выбирает набор эвристик по расширению файла. Неизвестное
// расширение означает отсутствие структурных границ и чистое текстовое
// разбиение.
func detector(path string) func([]line) []int {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown":
		return markdownBoundaries
	case ".go":
		return goBoundaries
	case ".py", ".pyi":
		return pythonBoundaries
	case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".mts", ".cts":
		return jsBoundaries
	case ".yaml", ".yml":
		return yamlBoundaries
	case ".json":
		return jsonBoundaries
	}
	return nil
}

// markdownBoundaries отмечает заголовки ATX. Внутри огороженного блока кода
// строка, начинающаяся с решётки, - это комментарий на другом языке, а не
// заголовок, поэтому такие блоки пропускаются целиком.
func markdownBoundaries(ls []line) []int {
	var out []int
	fence := ""
	for i, l := range ls {
		t := strings.TrimSpace(l.text)
		switch {
		case fence != "":
			if strings.HasPrefix(t, fence) {
				fence = ""
			}
			continue
		case strings.HasPrefix(t, "```"):
			fence = "```"
			continue
		case strings.HasPrefix(t, "~~~"):
			fence = "~~~"
			continue
		}
		if isATXHeading(t) {
			out = appendBoundary(out, i, ls)
		}
	}
	return out
}

// isATXHeading проверяет, что строка - заголовок вида "## Название".
func isATXHeading(t string) bool {
	n := 0
	for n < len(t) && t[n] == '#' {
		n++
	}
	if n == 0 || n > 6 || n == len(t) {
		return false
	}
	return t[n] == ' ' || t[n] == '\t'
}

// goBoundaries отмечает объявления верхнего уровня. Отступ означает
// вложенность: анонимная функция внутри тела - не самостоятельный блок.
func goBoundaries(ls []line) []int {
	return declBoundaries(ls, false,
		[]string{"func ", "type ", "const ", "var ", "func(", "//go:"},
		[]string{"//"})
}

// pythonBoundaries отмечает функции и классы, в том числе методы: отступ
// здесь не признак служебной вложенности, а обычная структура класса.
func pythonBoundaries(ls []line) []int {
	return declBoundaries(ls, true,
		[]string{"def ", "class ", "async def "},
		[]string{"#", "@"})
}

// jsBoundaries отмечает объявления функций, классов и экспортов.
func jsBoundaries(ls []line) []int {
	return declBoundaries(ls, false,
		[]string{
			"function ", "function*", "async function", "class ",
			"export ", "module.exports", "describe(", "it(", "test(",
		},
		[]string{"//", "/*", "*", "@"})
}

// yamlBoundaries отмечает верхнеуровневые ключи и элементы списка, а также
// разделители документов. Вложенные ключи идут с отступом и границами не
// считаются.
func yamlBoundaries(ls []line) []int {
	var out []int
	for i, l := range ls {
		t := l.text
		if t == "" || t[0] == ' ' || t[0] == '\t' || t[0] == '#' {
			continue
		}
		if strings.HasPrefix(t, "---") || strings.HasPrefix(t, "- ") || strings.Contains(t, ":") {
			out = appendBoundary(out, i, ls, "#")
		}
	}
	return out
}

// declBoundaries отмечает строки, начинающиеся с одного из префиксов.
//
// Если indented ложно, учитываются только строки без отступа. Строки,
// непосредственно предшествующие объявлению и начинающиеся с одного из
// attach-префиксов (комментарии, декораторы), включаются в тот же блок:
// документирующий комментарий описывает объявление и должен искаться вместе
// с ним.
func declBoundaries(ls []line, indented bool, prefixes, attach []string) []int {
	var out []int
	for i, l := range ls {
		t := l.text
		if !indented && (t == "" || t[0] == ' ' || t[0] == '\t') {
			continue
		}
		if indented {
			t = strings.TrimLeft(t, " \t")
		}
		if hasAnyPrefix(t, prefixes) {
			out = appendBoundary(out, i, ls, attach...)
		}
	}
	return out
}

// jsonBoundaries отмечает начала элементов верхнего уровня. Для файла в одну
// строку границ не будет: разбиение останется текстовым.
func jsonBoundaries(ls []line) []int {
	var out []int
	depth := 0
	inStr := false
	esc := false
	for i, l := range ls {
		if depth == 1 && !inStr {
			if t := strings.TrimLeft(l.text, " \t"); t != "" && !strings.HasPrefix(t, "}") && !strings.HasPrefix(t, "]") {
				out = appendBoundary(out, i, ls)
			}
		}
		for j := 0; j < len(l.text); j++ {
			c := l.text[j]
			switch {
			case esc:
				esc = false
			case inStr && c == '\\':
				esc = true
			case c == '"':
				inStr = !inStr
			case inStr:
			case c == '{' || c == '[':
				depth++
			case c == '}' || c == ']':
				depth--
			}
		}
		esc = false
	}
	return out
}

// appendBoundary добавляет смещение строки i, поднимаясь выше по
// предшествующим строкам с attach-префиксами. Границы первой строки файла и
// повторные значения отбрасываются: список должен строго возрастать.
func appendBoundary(out []int, i int, ls []line, attach ...string) []int {
	for i > 0 && len(attach) > 0 {
		prev := strings.TrimLeft(ls[i-1].text, " \t")
		if prev == "" || !hasAnyPrefix(prev, attach) {
			break
		}
		i--
	}
	if i == 0 {
		return out
	}
	off := ls[i].off
	if len(out) > 0 && out[len(out)-1] >= off {
		return out
	}
	return append(out, off)
}

// hasAnyPrefix сообщает, начинается ли строка с одного из префиксов.
func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
