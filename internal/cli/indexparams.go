package cli

import (
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
		return saveChunkParams(s, want)
	}
	if diff := chunkParamsDiff(stored, want); len(diff) > 0 {
		return fmt.Errorf(i18n.T(
			"index was built with other chunking parameters (%s); reindex from scratch (remove .senso) or repeat the parameters of the index",
			"индекс построен с другими параметрами нарезки (%s); переиндексируйте базу заново (удалите .senso) или повторите параметры индекса",
		), strings.Join(diff, ", "))
	}
	return nil
}
