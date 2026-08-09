package cli

import (
	"senso/internal/store"
)

// needsExpandedPool сообщает, активна ли хотя бы одна постобработка
// результатов - фильтры путей (--path/--ext/--exclude/--root),
// --deduplicate или --max-per-file. Это единая точка принятия решения о
// том, нужен ли searchPoolSize расширенный пул кандидатов: постобработка
// отбрасывает часть кандидатов, и без расширения пула после неё могло бы
// остаться меньше -k результатов.
func needsExpandedPool(filter *resultFilter, opts searchOptions) bool {
	return filter.Active() || opts.Deduplicate || opts.MaxPerFile > 0
}

// postProcessResults применяет к results постобработку, заданную opts:
// сначала --deduplicate, затем --max-per-file (в этом порядке, как описано
// в документации search). Неактивные опции возвращают results без
// изменений. Относительный порядок оставшихся результатов не меняется.
func postProcessResults(results []store.Result, opts searchOptions) []store.Result {
	if opts.Deduplicate {
		results = dedupeResults(results)
	}
	if opts.MaxPerFile > 0 {
		results = limitPerFile(results, opts.MaxPerFile)
	}
	return results
}

// dedupeResults подавляет соседние перекрывающиеся чанки одного и того же
// файла, включаемая флагом --deduplicate. Результаты уже отсортированы по
// релевантности (более релевантные раньше в списке), поэтому проход идёт
// слева направо: каждый результат сравнивается только с уже принятыми
// результатами того же файла, и из двух перекрывающихся остаётся тот, что
// был принят раньше (то есть более релевантный).
//
// Правило дублирования для пары результатов одного файла:
//   - если у обоих StartLine != 0, они считаются дубликатами, когда их
//     диапазоны строк [StartLine, EndLine] пересекаются;
//   - если у любого из двух StartLine == 0 (данных о строках нет),
//     используется запасное правило: результаты считаются дубликатами,
//     если разница их номеров чанков |Seq - Seq| <= 1 (соседние чанки).
//
// Результаты из разных файлов никогда не считаются дубликатами, даже если
// их текст совпадает.
func dedupeResults(results []store.Result) []store.Result {
	if len(results) == 0 {
		return results
	}

	// accepted группирует уже принятые результаты по пути файла - дубликат
	// может быть отброшен только при пересечении с результатом того же
	// файла, поэтому сравнивать с другими файлами не нужно.
	accepted := make(map[string][]store.Result)
	out := make([]store.Result, 0, len(results))

	for _, r := range results {
		duplicate := false
		for _, a := range accepted[r.Path] {
			if isOverlapping(a, r) {
				duplicate = true
				break
			}
		}
		if duplicate {
			continue
		}
		accepted[r.Path] = append(accepted[r.Path], r)
		out = append(out, r)
	}
	return out
}

// isOverlapping реализует правило дублирования, описанное в dedupeResults,
// для пары результатов одного файла a (уже принят) и b (кандидат).
func isOverlapping(a, b store.Result) bool {
	if a.StartLine == 0 || b.StartLine == 0 {
		diff := a.Seq - b.Seq
		if diff < 0 {
			diff = -diff
		}
		return diff <= 1
	}
	return a.StartLine <= b.EndLine && b.StartLine <= a.EndLine
}

// limitPerFile оставляет не более max результатов на каждый путь файла,
// включаемая флагом --max-per-file. Результаты уже отсортированы по
// релевантности, поэтому лишние (менее релевантные) отбрасываются с конца
// выдачи для каждого файла; относительный порядок оставшихся не меняется.
// max <= 0 означает отсутствие ограничения.
func limitPerFile(results []store.Result, max int) []store.Result {
	if max <= 0 {
		return results
	}

	counts := make(map[string]int)
	out := make([]store.Result, 0, len(results))
	for _, r := range results {
		if counts[r.Path] >= max {
			continue
		}
		counts[r.Path]++
		out = append(out, r)
	}
	return out
}
