// Package chunk разбивает текст документа на фрагменты для векторизации.
//
// Все размеры измеряются в рунах, а не в байтах: кириллица в UTF-8 занимает
// два байта на символ, иероглифы - три, поэтому байтовый лимит давал бы для
// разных языков куски разной смысловой ёмкости.
package chunk

import (
	"strings"
	"unicode/utf8"
)

// unit - неделимый кусок текста, гарантированно не превышающий целевой размер,
// вместе с разделителем, которым он присоединяется к предыдущему куску.
type unit struct {
	text string
	sep  string
}

// Split разбивает текст на фрагменты длиной около size рун с перекрытием
// overlap рун между соседями.
//
// Границы выбираются по убыванию предпочтительности: сначала абзацы, затем
// строки, и лишь в последнюю очередь - произвольная позиция внутри строки.
// Такой порядок сохраняет смысловую целостность фрагмента везде, где это
// возможно; жёсткий рез остаётся для вырожденных случаев вроде минифицированных
// файлов или длинных таблиц в одну строку.
//
// Фактическая длина фрагмента может достигать size+overlap: перекрытие
// добавляется к уже собранному фрагменту, а не урезает его полезную часть.
// Разделитель между хвостом и фрагментом учитывается внутри overlap.
// Пустые и целиком пробельные фрагменты не возвращаются.
func Split(text string, size, overlap int) []string {
	if size <= 0 {
		return nil
	}
	if overlap < 0 {
		overlap = 0
	}
	// Перекрытие в размер фрагмента означало бы, что каждый следующий
	// фрагмент целиком повторяет предыдущий и индекс перестаёт расти.
	if overlap >= size {
		overlap = size / 2
	}

	units := splitUnits(text, size)
	packed := pack(units, size)
	return applyOverlap(packed, overlap)
}

// splitUnits разбирает текст на куски, каждый из которых помещается в size.
func splitUnits(text string, size int) []unit {
	var units []unit
	for i, para := range strings.Split(text, "\n\n") {
		sep := "\n\n"
		if i == 0 {
			sep = ""
		}
		if strings.TrimSpace(para) == "" {
			continue
		}
		if utf8.RuneCountInString(para) <= size {
			units = append(units, unit{para, sep})
			continue
		}
		// Абзац не помещается целиком - опускаемся до строк.
		for j, line := range strings.Split(para, "\n") {
			lineSep := "\n"
			if j == 0 {
				lineSep = sep
			}
			if utf8.RuneCountInString(line) <= size {
				units = append(units, unit{line, lineSep})
				continue
			}
			// И строка не помещается - режем по счётчику рун.
			for k, piece := range hardSplit(line, size) {
				pieceSep := ""
				if k == 0 {
					pieceSep = lineSep
				}
				units = append(units, unit{piece, pieceSep})
			}
		}
	}
	return units
}

// hardSplit режет строку на куски ровно по size рун, не разрывая руну.
func hardSplit(s string, size int) []string {
	var out []string
	var b strings.Builder
	n := 0
	for _, r := range s {
		b.WriteRune(r)
		n++
		if n == size {
			out = append(out, b.String())
			b.Reset()
			n = 0
		}
	}
	if b.Len() > 0 {
		out = append(out, b.String())
	}
	return out
}

// pack жадно склеивает куски, пока результат помещается в size.
func pack(units []unit, size int) []string {
	var out []string
	var cur strings.Builder
	curLen := 0

	flush := func() {
		if strings.TrimSpace(cur.String()) != "" {
			out = append(out, cur.String())
		}
		cur.Reset()
		curLen = 0
	}

	for _, u := range units {
		uLen := utf8.RuneCountInString(u.text)
		sep := u.sep
		if curLen == 0 {
			sep = ""
		}
		sepLen := utf8.RuneCountInString(sep)

		if curLen > 0 && curLen+sepLen+uLen > size {
			flush()
			sep, sepLen = "", 0
		}
		cur.WriteString(sep)
		cur.WriteString(u.text)
		curLen += sepLen + uLen
	}
	flush()
	return out
}

// applyOverlap приписывает каждому фрагменту хвост предыдущего.
//
// Перекрытие нужно, чтобы мысль, оказавшаяся на стыке двух фрагментов,
// целиком присутствовала хотя бы в одном из них и была найдена поиском.
func applyOverlap(chunks []string, overlap int) []string {
	// Перекрытие в одну руну не переносит смысла, а разделитель уже занял бы
	// весь бюджет, поэтому такой случай приравнивается к его отсутствию.
	if overlap < 2 || len(chunks) < 2 {
		return chunks
	}
	// Перевод строки, склеивающий хвост с фрагментом, входит в бюджет
	// перекрытия: иначе фрагмент оказался бы длиннее объявленного предела.
	tailLen := overlap - 1
	out := make([]string, 0, len(chunks))
	out = append(out, chunks[0])
	for i := 1; i < len(chunks); i++ {
		tail := lastRunes(chunks[i-1], tailLen)
		if tail == "" {
			out = append(out, chunks[i])
			continue
		}
		out = append(out, tail+"\n"+chunks[i])
	}
	return out
}

// lastRunes возвращает последние n рун строки, сдвигая начало к ближайшей
// границе слова, чтобы перекрытие не начиналось с обрубка.
func lastRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return strings.TrimSpace(s)
	}
	tail := string(r[len(r)-n:])
	// Обрубок в начале хвоста отбрасывается, но только если после него
	// остаётся заметная часть текста.
	if idx := strings.IndexAny(tail, " \t\n"); idx > 0 && idx < len(tail)/2 {
		tail = tail[idx+1:]
	}
	return strings.TrimSpace(tail)
}
