package cli

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"senso/internal/i18n"
	"senso/internal/store"
)

// Ключи meta, под которыми хранятся параметры разбиения на чанки. Они
// нужны, чтобы повторная индексация не смешивала в одной базе чанки,
// нарезанные разными способами: файл, проиндексированный с одними
// параметрами, и файл с другими дают несопоставимые границы и диапазоны
// строк, а по индексу это никак не видно.
const (
	metaChunker   = "chunker"
	metaChunkSize = "chunk_size"
	metaOverlap   = "overlap"
	// metaSelection - правила отбора файлов в виде JSON: check берёт их
	// как значения по умолчанию, чтобы не требовать повторения флагов
	// индексации.
	metaSelection = "selection"
)

// chunkParams - параметры нарезки, влияющие на содержимое индекса.
type chunkParams struct {
	Chunker   string
	ChunkSize int
	Overlap   int
}

// wantChunkParams возвращает параметры нарезки, заданные флагами команды.
func wantChunkParams(opts indexOptions) chunkParams {
	return chunkParams{Chunker: opts.Chunker, ChunkSize: opts.ChunkSize, Overlap: opts.Overlap}
}

// loadChunkParams читает параметры нарезки из базы. Второе возвращаемое
// значение - признак того, что параметры записаны: базы, созданные senso до
// появления этих ключей, их не содержат, и сравнивать там не с чем.
func loadChunkParams(s *store.Store) (chunkParams, bool, error) {
	chunker, err := s.GetMeta(metaChunker)
	if err != nil {
		return chunkParams{}, false, err
	}
	if chunker == "" {
		return chunkParams{}, false, nil
	}
	size, err := metaInt(s, metaChunkSize)
	if err != nil {
		return chunkParams{}, false, err
	}
	overlap, err := metaInt(s, metaOverlap)
	if err != nil {
		return chunkParams{}, false, err
	}
	return chunkParams{Chunker: chunker, ChunkSize: size, Overlap: overlap}, true, nil
}

// metaInt читает целочисленное значение из meta; отсутствующий или
// нечисловой ключ даёт 0, а не ошибку - параметры нарезки не настолько
// важны, чтобы из-за них падала вся команда.
func metaInt(s *store.Store, key string) (int, error) {
	raw, err := s.GetMeta(key)
	if err != nil {
		return 0, err
	}
	n, convErr := strconv.Atoi(raw)
	if convErr != nil {
		return 0, nil
	}
	return n, nil
}

// saveChunkParams записывает параметры нарезки в meta.
func saveChunkParams(s *store.Store, p chunkParams) error {
	if err := s.SetMeta(metaChunker, p.Chunker); err != nil {
		return err
	}
	if err := s.SetMeta(metaChunkSize, strconv.Itoa(p.ChunkSize)); err != nil {
		return err
	}
	return s.SetMeta(metaOverlap, strconv.Itoa(p.Overlap))
}

// chunkParamsDiff перечисляет расхождения между параметрами базы и
// запрошенными: пустой результат означает, что индексировать можно, ничего
// не смешивая.
func chunkParamsDiff(stored, want chunkParams) []string {
	var diff []string
	if stored.Chunker != want.Chunker {
		diff = append(diff, fmt.Sprintf("--chunker %s != %s", stored.Chunker, want.Chunker))
	}
	if stored.ChunkSize != want.ChunkSize {
		diff = append(diff, fmt.Sprintf("--chunk-size %d != %d", stored.ChunkSize, want.ChunkSize))
	}
	if stored.Overlap != want.Overlap {
		diff = append(diff, fmt.Sprintf("--overlap %d != %d", stored.Overlap, want.Overlap))
	}
	return diff
}

// ensureChunkParams согласует параметры нарезки с уже существующей базой.
// Если параметры записаны и не совпадают с запрошенными, индексация
// прекращается: senso не умеет пересчитывать нетронутые файлы, поэтому
// молчаливое продолжение оставило бы в базе чанки двух разных нарезок.
// Базам без записанных параметров они просто проставляются.
func ensureChunkParams(s *store.Store, opts indexOptions) error {
	want := wantChunkParams(opts)
	stored, ok, err := loadChunkParams(s)
	if err != nil {
		return err
	}
	if !ok {
		return saveIndexParams(s, opts)
	}
	if diff := chunkParamsDiff(stored, want); len(diff) > 0 {
		return fmt.Errorf(i18n.T(
			"index was built with other chunking parameters (%s); reindex from scratch (remove .senso) or repeat the parameters of the index",
			"индекс построен с другими параметрами нарезки (%s); переиндексируйте базу заново (удалите .senso) или повторите параметры индекса",
		), strings.Join(diff, ", "))
	}
	return saveSelectionParams(s, wantSelectionParams(opts))
}

// selectionParams - правила отбора файлов, с которыми построен индекс.
// Хранятся одним JSON-значением: набор флагов меняется от версии к версии,
// а отдельный ключ meta на каждый флаг быстро превратился бы в свалку.
type selectionParams struct {
	Ext           string `json:"ext,omitempty"`
	Exclude       string `json:"exclude,omitempty"`
	NoGitignore   bool   `json:"no_gitignore,omitempty"`
	Hidden        bool   `json:"hidden,omitempty"`
	IncludeHidden string `json:"include_hidden,omitempty"`
	Noisy         bool   `json:"noisy,omitempty"`
	IncludeNoisy  string `json:"include_noisy,omitempty"`
	NoisyPatterns string `json:"noisy_patterns,omitempty"`
	MaxFileSize   int    `json:"max_file_size,omitempty"`
}

// wantSelectionParams собирает правила отбора из флагов команды.
func wantSelectionParams(opts indexOptions) selectionParams {
	return selectionParams{
		Ext:           opts.Ext,
		Exclude:       opts.Exclude,
		NoGitignore:   opts.NoGitignore,
		Hidden:        opts.Hidden,
		IncludeHidden: opts.IncludeHidden,
		Noisy:         opts.Noisy,
		IncludeNoisy:  opts.IncludeNoisy,
		NoisyPatterns: opts.NoisyPatterns,
		MaxFileSize:   opts.MaxFileSize,
	}
}

// loadSelectionParams читает правила отбора из базы. Второе возвращаемое
// значение - признак того, что правила записаны: у баз, созданных без них,
// подставлять нечего.
func loadSelectionParams(s *store.Store) (selectionParams, bool, error) {
	raw, err := s.GetMeta(metaSelection)
	if err != nil || raw == "" {
		return selectionParams{}, false, err
	}
	var sel selectionParams
	if err := json.Unmarshal([]byte(raw), &sel); err != nil {
		// Испорченная запись - не повод отказывать в проверке: просто
		// работаем по флагам команды, как раньше.
		return selectionParams{}, false, nil
	}
	return sel, true, nil
}

// saveSelectionParams записывает правила отбора в meta.
func saveSelectionParams(s *store.Store, sel selectionParams) error {
	data, err := json.Marshal(sel)
	if err != nil {
		return err
	}
	return s.SetMeta(metaSelection, string(data))
}

// saveIndexParams сохраняет в meta все параметры, влияющие на состав
// индекса: нарезку и правила отбора файлов.
func saveIndexParams(s *store.Store, opts indexOptions) error {
	if err := saveChunkParams(s, wantChunkParams(opts)); err != nil {
		return err
	}
	return saveSelectionParams(s, wantSelectionParams(opts))
}

// applyStoredParams подставляет параметры индексации из базы вместо флагов,
// которые пользователь не задал явно. Без этого "senso check" пришлось бы
// каждый раз повторять флаги индексации: иначе, например, скрытые файлы
// индекса выглядели бы исключёнными и проверка ложно считала бы его
// устаревшим. Явно заданный флаг всегда сильнее записи в базе - так
// проверяют намерение переиндексировать дерево по другим правилам.
func applyStoredParams(s *store.Store, opts *checkOptions) error {
	setByUser := func(name string) bool { return opts.setFlags[name] }

	if chunkP, ok, err := loadChunkParams(s); err != nil {
		return err
	} else if ok {
		if !setByUser("chunker") {
			opts.Chunker = chunkP.Chunker
		}
		if !setByUser("chunk-size") {
			opts.ChunkSize = chunkP.ChunkSize
		}
		if !setByUser("overlap") {
			opts.Overlap = chunkP.Overlap
		}
	}

	sel, ok, err := loadSelectionParams(s)
	if err != nil || !ok {
		return err
	}

	if !setByUser("ext") {
		opts.Ext = sel.Ext
	}
	if !setByUser("exclude") {
		opts.Exclude = sel.Exclude
	}
	if !setByUser("no-gitignore") {
		opts.NoGitignore = sel.NoGitignore
	}
	if !setByUser("hidden") {
		opts.Hidden = sel.Hidden
	}
	if !setByUser("include-hidden") {
		opts.IncludeHidden = sel.IncludeHidden
	}
	if !setByUser("noisy") {
		opts.Noisy = sel.Noisy
	}
	if !setByUser("include-noisy") {
		opts.IncludeNoisy = sel.IncludeNoisy
	}
	if !setByUser("noisy-patterns") {
		opts.NoisyPatterns = sel.NoisyPatterns
	}
	if !setByUser("max-file-size") && sel.MaxFileSize > 0 {
		opts.MaxFileSize = sel.MaxFileSize
	}
	return nil
}
