// Package chunk разбивает текст документа на фрагменты для векторизации.
//
// Все размеры измеряются в рунах, а не в байтах: кириллица в UTF-8 занимает
// два байта на символ, иероглифы - три, поэтому байтовый лимит давал бы для
// разных языков куски разной смысловой ёмкости.
package chunk

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// Chunk - фрагмент документа вместе с диапазоном строк исходного файла,
// который он покрывает. Строки нумеруются с единицы, EndLine включительна.
//
// Диапазон охватывает весь текст фрагмента, включая перекрытие с предыдущим
// фрагментом: по нему можно открыть файл на нужном месте и увидеть ровно то,
// что попало в индекс.
type Chunk struct {
	Text      string
	StartLine int
	EndLine   int
}

// unit - неделимый кусок текста, гарантированно не превышающий целевой размер,
// вместе с разделителем, которым он присоединяется к предыдущему куску,
// и байтовым смещением в исходном тексте.
type unit struct {
	text string
	sep  string
	off  int
}

// span - собранный фрагмент вместе с полуинтервалом [off, end) байтовых
// смещений в исходном тексте, из которого он склеен.
type span struct {
	text string
	off  int
	end  int
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
//
// Каждый возвращённый Chunk несёт диапазон строк исходного text, который он
// покрывает; диапазон включает и перекрытие с предыдущим фрагментом.
func Split(text string, size, overlap int) []Chunk {
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
	spans := applyOverlap(packed, overlap)

	nl := newlineOffsets(text)
	chunks := make([]Chunk, len(spans))
	for i, s := range spans {
		end := s.end - 1
		if end < s.off {
			end = s.off
		}
		chunks[i] = Chunk{
			Text:      s.text,
			StartLine: lineAt(nl, s.off),
			EndLine:   lineAt(nl, end),
		}
	}
	return chunks
}

// splitUnits разбирает текст на куски, каждый из которых помещается в size.
func splitUnits(text string, size int) []unit {
	var units []unit
	paraOff := 0
	for i, para := range strings.Split(text, "\n\n") {
		off := paraOff
		paraOff += len(para) + 2
		sep := "\n\n"
		if i == 0 {
			sep = ""
		}
		if strings.TrimSpace(para) == "" {
			continue
		}
		if utf8.RuneCountInString(para) <= size {
			units = append(units, unit{para, sep, off})
			continue
		}
		// Абзац не помещается целиком - опускаемся до строк.
		lineOff := off
		for j, line := range strings.Split(para, "\n") {
			lo := lineOff
			lineOff += len(line) + 1
			lineSep := "\n"
			if j == 0 {
				lineSep = sep
			}
			if utf8.RuneCountInString(line) <= size {
				units = append(units, unit{line, lineSep, lo})
				continue
			}
			// И строка не помещается - режем по счётчику рун.
			pieceOff := lo
			for k, piece := range hardSplit(line, size) {
				pieceSep := ""
				if k == 0 {
					pieceSep = lineSep
				}
				units = append(units, unit{piece, pieceSep, pieceOff})
				pieceOff += len(piece)
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
func pack(units []unit, size int) []span {
	var out []span
	var cur strings.Builder
	curLen := 0
	curOff := 0
	curEnd := 0

	flush := func() {
		if strings.TrimSpace(cur.String()) != "" {
			out = append(out, span{cur.String(), curOff, curEnd})
		}
		cur.Reset()
		curLen = 0
	}

	for _, u := range units {
		uLen := utf8.RuneCountInString(u.text)
		sep := u.sep
		if curLen == 0 {
			sep = ""
			curOff = u.off
		}
		sepLen := utf8.RuneCountInString(sep)

		if curLen > 0 && curLen+sepLen+uLen > size {
			flush()
			sep, sepLen = "", 0
			curOff = u.off
		}
		cur.WriteString(sep)
		cur.WriteString(u.text)
		curLen += sepLen + uLen
		curEnd = u.off + len(u.text)
	}
	flush()
	return out
}

// applyOverlap приписывает каждому фрагменту хвост предыдущего.
//
// Перекрытие нужно, чтобы мысль, оказавшаяся на стыке двух фрагментов,
// целиком присутствовала хотя бы в одном из них и была найдена поиском.
func applyOverlap(spans []span, overlap int) []span {
	// Перекрытие в одну руну не переносит смысла, а разделитель уже занял бы
	// весь бюджет, поэтому такой случай приравнивается к его отсутствию.
	if overlap < 2 || len(spans) < 2 {
		return spans
	}
	// Перевод строки, склеивающий хвост с фрагментом, входит в бюджет
	// перекрытия: иначе фрагмент оказался бы длиннее объявленного предела.
	tailLen := overlap - 1
	out := make([]span, 0, len(spans))
	out = append(out, spans[0])
	for i := 1; i < len(spans); i++ {
		tail := lastRunes(spans[i-1].text, tailLen)
		if tail == "" {
			out = append(out, spans[i])
			continue
		}
		// tail - это всегда непрерывная подстрока текста предыдущего
		// фрагмента (lastRunes только отрезает края), поэтому её позицию
		// можно найти обратным поиском и вычислить, откуда в исходном
		// тексте реально начинается склеенный фрагмент.
		off := spans[i].off
		if idx := strings.LastIndex(spans[i-1].text, tail); idx >= 0 {
			off = spans[i-1].off + idx
		}
		out = append(out, span{tail + "\n" + spans[i].text, off, spans[i].end})
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

// newlineOffsets возвращает байтовые смещения всех переводов строк text.
func newlineOffsets(text string) []int {
	var nl []int
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			nl = append(nl, i)
		}
	}
	return nl
}

// lineAt возвращает 1-based номер строки, в которой лежит байт off.
func lineAt(nl []int, off int) int { return sort.SearchInts(nl, off) + 1 }
