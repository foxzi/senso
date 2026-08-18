// Package store отвечает за хранение и поиск индекса senso в SQLite с
// расширением sqlite-vec: файлы, их чанки текста и векторные эмбеддинги.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"senso/internal/chunk"
	"senso/internal/i18n"
	"senso/internal/stem"
	"senso/internal/vecext"
)

func init() {
	// Регистрируем расширение sqlite-vec для всех новых соединений до
	// первого открытия базы данных.
	vecext.Auto()
}

// schemaVersion - текущая версия схемы, хранится в meta.schema_version.
const schemaVersion = "4"

// Store - соединение с базой данных senso.
type Store struct {
	db *sql.DB
}

// Open открывает (или создаёт) файл базы данных по пути path и настраивает
// прагмы соединения. Схему нужно инициализировать отдельно вызовом Init.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}
	// vec0 не работает с несколькими одновременными соединениями к одной
	// базе так же надёжно, как обычные таблицы, а senso - консольная
	// утилита без конкурентного доступа, поэтому ограничиваем один коннект.
	db.SetMaxOpenConns(1)

	pragmas := []string{
		"PRAGMA journal_mode=WAL",
		"PRAGMA synchronous=NORMAL",
		"PRAGMA foreign_keys=ON",
	}
	for _, p := range pragmas {
		if _, err := db.Exec(p); err != nil {
			db.Close()
			return nil, fmt.Errorf("store: %s: %w", p, err)
		}
	}

	return &Store{db: db}, nil
}

// OpenReadOnly открывает существующую базу без права записи: SQLite сам
// отклонит любую попытку изменения, поэтому диагностические команды
// физически не могут повредить индекс. Файл должен существовать - режим
// mode=ro не создаёт базу.
func OpenReadOnly(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", "file:"+path+"?mode=ro")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)

	// journal_mode и synchronous в режиме только чтения задать нельзя -
	// это запись в саму базу; foreign_keys живёт в соединении и безопасен.
	if _, err := db.Exec("PRAGMA foreign_keys=ON"); err != nil {
		db.Close()
		return nil, fmt.Errorf("store: PRAGMA foreign_keys=ON: %w", err)
	}

	return &Store{db: db}, nil
}

// Close закрывает соединение с базой данных.
func (s *Store) Close() error {
	return s.db.Close()
}

const schemaDDL = `
CREATE TABLE meta(key TEXT PRIMARY KEY, value TEXT);
CREATE TABLE files(
  id INTEGER PRIMARY KEY,
  path TEXT NOT NULL UNIQUE,
  mtime INTEGER NOT NULL,
  size INTEGER NOT NULL,
  hash TEXT NOT NULL);
-- line_start/line_end - диапазон строк исходного файла (1-based, line_end
-- включительно), который покрывает чанк. Базы, созданные до появления этих
-- колонок, несовместимы по schemaVersion и требуют переиндексации: номера
-- строк нельзя восстановить задним числом без повторного чтения файлов.
CREATE TABLE chunks(
  id INTEGER PRIMARY KEY,
  file_id INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
  seq INTEGER NOT NULL,
  text TEXT NOT NULL,
  line_start INTEGER NOT NULL DEFAULT 0,
  line_end INTEGER NOT NULL DEFAULT 0,
  UNIQUE(file_id, seq));
CREATE INDEX idx_chunks_file ON chunks(file_id);
-- body - текст чанка, path - путь к файлу, ids - слова составных
-- идентификаторов (см. stem.Idents). Колонки разделены, чтобы фразовый
-- поиск по тексту не сбивался вставками из пути и идентификаторов, а вклад
-- каждой колонки в релевантность можно было взвесить отдельно (bm25Weights).
CREATE VIRTUAL TABLE IF NOT EXISTS fts_chunks USING fts5(
    body,
    path,
    ids,
    chunk_id UNINDEXED,
    tokenize="unicode61 remove_diacritics 2",
    prefix='2 3'
);
`

// Init создаёт схему при первом запуске (если таблица meta ещё не
// существует) и фиксирует модель эмбеддингов вместе с размерностью и
// корневой директорией индекса. Пустая модель ("") и dim=0 допустимы - это
// чисто лексический индекс без векторов. Если схема уже существует и в ней
// сохранена непустая модель, проверяет, что она совпадает с переданной - при
// расхождении возвращает ошибку.
func (s *Store) Init(model string, dim int, root string) error {
	exists, err := s.tableExists("meta")
	if err != nil {
		return err
	}

	if !exists {
		return s.createSchema(model, dim, root)
	}

	if err := s.CheckSchema(); err != nil {
		return err
	}

	curModel, curDim, err := s.Meta()
	if err != nil {
		return err
	}
	if curModel != "" && (curModel != model || curDim != dim) {
		return errors.New(i18n.Tf(
			"index built with model %s (dim %d); delete the database or specify --model %s",
			"индекс построен моделью %s (dim %d); удалите базу данных или укажите --model %s",
			curModel, curDim, curModel,
		))
	}
	return nil
}

// CheckSchema проверяет, что версия схемы существующей базы совместима с
// текущей версией senso. Для ещё не инициализированной базы (таблицы meta
// нет) проверять нечего - возвращает nil.
func (s *Store) CheckSchema() error {
	exists, err := s.tableExists("meta")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}

	curVersion, err := s.GetMeta("schema_version")
	if err != nil {
		return err
	}
	if curVersion != schemaVersion {
		return errors.New(i18n.Tf(
			"store: database created by an incompatible senso version (schema %s, requires %s), delete the .senso directory and reindex",
			"store: база создана несовместимой версией senso (схема %s, требуется %s), удалите каталог .senso и выполните индексацию заново",
			curVersion, schemaVersion,
		))
	}
	return nil
}

// tableExists проверяет наличие таблицы name в текущей базе данных.
func (s *Store) tableExists(name string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&n)
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// createSchema создаёт таблицы схемы (без vec_chunks - её размерность
// известна только после первого эмбеддинга, см. EnsureVectors) и
// записывает начальные значения meta.
func (s *Store) createSchema(model string, dim int, root string) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(schemaDDL); err != nil {
		return fmt.Errorf(i18n.T("store: create schema: %w", "store: создание схемы: %w"), err)
	}

	meta := map[string]string{
		"schema_version": schemaVersion,
		"model":          model,
		"dim":            fmt.Sprintf("%d", dim),
		"root":           root,
		"indexed_at":     "",
	}
	for k, v := range meta {
		if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES (?, ?)`, k, v); err != nil {
			return fmt.Errorf(i18n.T("store: write meta.%s: %w", "store: запись meta.%s: %w"), k, err)
		}
	}

	return tx.Commit()
}

// Meta возвращает модель эмбеддингов и её размерность, записанные в базе.
func (s *Store) Meta() (model string, dim int, err error) {
	if err = s.db.QueryRow(`SELECT value FROM meta WHERE key='model'`).Scan(&model); err != nil {
		return "", 0, fmt.Errorf(i18n.T("store: read meta.model: %w", "store: чтение meta.model: %w"), err)
	}
	var dimStr string
	if err = s.db.QueryRow(`SELECT value FROM meta WHERE key='dim'`).Scan(&dimStr); err != nil {
		return "", 0, fmt.Errorf(i18n.T("store: read meta.dim: %w", "store: чтение meta.dim: %w"), err)
	}
	if dim, err = strconv.Atoi(dimStr); err != nil {
		return "", 0, fmt.Errorf(i18n.T("store: parse meta.dim=%q: %w", "store: разбор meta.dim=%q: %w"), dimStr, err)
	}
	return model, dim, nil
}

// SetMeta сохраняет произвольный ключ в таблицу meta (upsert).
func (s *Store) SetMeta(key, value string) error {
	if _, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
		return fmt.Errorf(i18n.T("store: write meta.%s: %w", "store: запись meta.%s: %w"), key, err)
	}
	return nil
}

// GetMeta возвращает значение ключа из таблицы meta; отсутствие ключа - это
// не ошибка, возвращается пустая строка.
func (s *Store) GetMeta(key string) (string, error) {
	var value string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf(i18n.T("store: read meta.%s: %w", "store: чтение meta.%s: %w"), key, err)
	}
	return value, nil
}

// rootsMetaKey - ключ в meta, под которым хранится JSON-массив абсолютных
// путей всех корней, когда-либо проиндексированных в эту базу. Отдельного
// ключа заведомо достаточно: схему менять нельзя (см. schemaVersion), а
// meta - готовое хранилище ключ/значение.
const rootsMetaKey = "roots"

// Roots возвращает список абсолютных путей всех корней, зарегистрированных
// в этой базе через AddRoot. Для баз, созданных до появления meta.roots,
// возвращается список из одного элемента - значения meta.root; если и его
// нет, возвращается пустой список без ошибки.
func (s *Store) Roots() ([]string, error) {
	raw, err := s.GetMeta(rootsMetaKey)
	if err != nil {
		return nil, err
	}
	if raw != "" {
		var roots []string
		if err := json.Unmarshal([]byte(raw), &roots); err != nil {
			return nil, fmt.Errorf(i18n.T("store: parse meta.roots: %w", "store: разбор meta.roots: %w"), err)
		}
		return roots, nil
	}

	// Старая база без meta.roots - единственный известный корень лежит в
	// meta.root (его пишет Init при создании схемы).
	root, err := s.GetMeta("root")
	if err != nil {
		return nil, err
	}
	if root == "" {
		return []string{}, nil
	}
	return []string{root}, nil
}

// AddRoot регистрирует абсолютный путь root в списке проиндексированных
// корней базы (meta.roots). Если root уже покрыт одним из записанных
// корней (совпадает с ним или вложен в него), список не меняется. Если
// root, наоборот, является предком уже записанных корней, эти вложенные
// корни поглощаются им и убираются из списка - индексация родителя
// покрывает и их. Повторный вызов с тем же root не создаёт дублей.
func (s *Store) AddRoot(root string) error {
	root = filepath.Clean(root)

	roots, err := s.Roots()
	if err != nil {
		return err
	}

	for _, r := range roots {
		if pathInSubtree(root, r) {
			return nil
		}
	}

	var updated []string
	for _, r := range roots {
		if pathInSubtree(r, root) {
			// r вложен в новый root - поглощается им.
			continue
		}
		updated = append(updated, r)
	}
	updated = append(updated, root)

	data, err := json.Marshal(updated)
	if err != nil {
		return err
	}
	return s.SetMeta(rootsMetaKey, string(data))
}

// RemoveRoot убирает из meta.roots все корни, которые равны prefix или
// лежат внутри поддерева prefix. Вызывается после удаления файлов из индекса,
// чтобы список корней не накапливал записи, за которыми уже нет ни одного
// файла. Возвращает признак того, что список действительно изменился.
//
// Корень, внутри которого лежит prefix (то есть предок удаляемого пути),
// не трогается: из него удалили лишь часть поддерева, сам он остаётся
// проиндексированным.
func (s *Store) RemoveRoot(prefix string) (bool, error) {
	prefix = filepath.Clean(prefix)

	roots, err := s.Roots()
	if err != nil {
		return false, err
	}

	kept := make([]string, 0, len(roots))
	for _, r := range roots {
		if pathInSubtree(r, prefix) {
			continue
		}
		kept = append(kept, r)
	}
	if len(kept) == len(roots) {
		return false, nil
	}

	data, err := json.Marshal(kept)
	if err != nil {
		return false, err
	}
	return true, s.SetMeta(rootsMetaKey, string(data))
}

// pathInSubtree сообщает, лежит ли path внутри поддерева root (или равен
// ему). Сравнение идёт по границам сегментов пути, поэтому "/a/bc" не
// считается вложенным в "/a/b". По смыслу совпадает с cli.prefixInSubtree,
// но продублирована здесь, чтобы store не зависел от cli.
func pathInSubtree(path, root string) bool {
	path = filepath.Clean(path)
	root = filepath.Clean(root)
	if path == root {
		return true
	}
	return strings.HasPrefix(path, root+string(filepath.Separator))
}

// execer - минимальный интерфейс для выполнения DDL/DML, реализуемый и
// *sql.DB, и *sql.Tx. Позволяет вызывать ensureVectorsExec как отдельно,
// так и внутри уже открытой транзакции.
type execer interface {
	Exec(query string, args ...any) (sql.Result, error)
}

// ensureVectorsExec идемпотентно создаёт таблицу vec_chunks с размерностью
// dim через execer e (соединение или транзакцию). IF NOT EXISTS делает вызов
// безопасным при повторном использовании, в том числе внутри транзакции, где
// отдельная проверка через tableExists привела бы к взаимоблокировке
// (соединение к базе одно, а Store.db занят открытой Tx).
func ensureVectorsExec(e execer, dim int) error {
	vecDDL := fmt.Sprintf(
		`CREATE VIRTUAL TABLE IF NOT EXISTS vec_chunks USING vec0(chunk_id INTEGER PRIMARY KEY, embedding float[%d] distance_metric=cosine)`,
		dim,
	)
	if _, err := e.Exec(vecDDL); err != nil {
		return fmt.Errorf(i18n.T("store: create vec_chunks: %w", "store: создание vec_chunks: %w"), err)
	}
	return nil
}

// EnsureVectors идемпотентно создаёт таблицу vec_chunks с размерностью dim,
// если она ещё не существует. Нужна для отложенного создания векторного
// индекса - при первой индексации с эмбеддингами.
func (s *Store) EnsureVectors(dim int) error {
	exists, err := s.tableExists("vec_chunks")
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return ensureVectorsExec(s.db, dim)
}

// HasVectors сообщает, создана ли уже таблица векторов vec_chunks.
func (s *Store) HasVectors() (bool, error) {
	return s.tableExists("vec_chunks")
}

// FileState возвращает сохранённые mtime, size и hash файла path.
// found=false, если файл ещё не индексировался.
func (s *Store) FileState(path string) (mtime, size int64, hash string, found bool, err error) {
	err = s.db.QueryRow(`SELECT mtime, size, hash FROM files WHERE path=?`, path).Scan(&mtime, &size, &hash)
	if err == sql.ErrNoRows {
		return 0, 0, "", false, nil
	}
	if err != nil {
		return 0, 0, "", false, err
	}
	return mtime, size, hash, true, nil
}

// TouchFile обновляет mtime и size файла path, не трогая его чанки.
// Используется, когда содержимое файла не изменилось (хэш совпал), но
// нужно зафиксировать новые метаданные файловой системы.
func (s *Store) TouchFile(path string, mtime, size int64) error {
	res, err := s.db.Exec(`UPDATE files SET mtime=?, size=? WHERE path=?`, mtime, size, path)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return errors.New(i18n.Tf("store: TouchFile: file %q not found", "store: TouchFile: файл %q не найден", path))
	}
	return nil
}

// ReplaceFile в одной транзакции удаляет прежние чанки файла path вместе с
// их векторами и записывает новые chunks/vectors. Если файла ещё не было,
// создаёт его.
func (s *Store) ReplaceFile(path string, mtime, size int64, hash string, chunks []chunk.Chunk, vectors [][]float32) error {
	if len(vectors) > 0 && len(chunks) != len(vectors) {
		return errors.New(i18n.Tf(
			"store: ReplaceFile: chunk count (%d) does not match vector count (%d)",
			"store: ReplaceFile: количество чанков (%d) не совпадает с количеством векторов (%d)",
			len(chunks), len(vectors),
		))
	}

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`INSERT INTO files(path, mtime, size, hash) VALUES (?, ?, ?, ?)
		 ON CONFLICT(path) DO UPDATE SET mtime=excluded.mtime, size=excluded.size, hash=excluded.hash`,
		path, mtime, size, hash,
	); err != nil {
		return fmt.Errorf("store: upsert files: %w", err)
	}
	// LastInsertId() ненадёжен при ON CONFLICT DO UPDATE (зависит от
	// версии SQLite и драйвера), поэтому id всегда получаем отдельным
	// запросом.
	var fileID int64
	if err := tx.QueryRow(`SELECT id FROM files WHERE path=?`, path).Scan(&fileID); err != nil {
		return fmt.Errorf(i18n.T("store: get file_id: %w", "store: получение file_id: %w"), err)
	}

	// vec_chunks не поддерживает внешние ключи - удаляем векторы явно по
	// chunk_id, прежде чем удалить сами чанки. Таблица может ещё не
	// существовать (лексический индекс без эмбеддингов), тогда удаление
	// пропускаем.
	rows, err := tx.Query(`SELECT id FROM chunks WHERE file_id=?`, fileID)
	if err != nil {
		return fmt.Errorf(i18n.T("store: select old chunk_id: %w", "store: выборка старых chunk_id: %w"), err)
	}
	var oldChunkIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		oldChunkIDs = append(oldChunkIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	if len(oldChunkIDs) > 0 {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='vec_chunks'`).Scan(&n); err != nil {
			return fmt.Errorf(i18n.T("store: check vec_chunks: %w", "store: проверка vec_chunks: %w"), err)
		}
		if n > 0 {
			for _, id := range oldChunkIDs {
				if _, err := tx.Exec(`DELETE FROM vec_chunks WHERE chunk_id=?`, id); err != nil {
					return fmt.Errorf(i18n.T("store: delete vector chunk_id=%d: %w", "store: удаление вектора chunk_id=%d: %w"), id, err)
				}
			}
		}
		for _, id := range oldChunkIDs {
			if _, err := tx.Exec(`DELETE FROM fts_chunks WHERE chunk_id=?`, id); err != nil {
				return fmt.Errorf(i18n.T("store: delete from fts_chunks chunk_id=%d: %w", "store: удаление из fts_chunks chunk_id=%d: %w"), id, err)
			}
		}
	}
	if _, err := tx.Exec(`DELETE FROM chunks WHERE file_id=?`, fileID); err != nil {
		return fmt.Errorf(i18n.T("store: delete old chunks: %w", "store: удаление старых chunks: %w"), err)
	}

	if len(vectors) > 0 {
		if err := ensureVectorsExec(tx, len(vectors[0])); err != nil {
			return err
		}
	}

	for i, c := range chunks {
		res, err := tx.Exec(`INSERT INTO chunks(file_id, seq, text, line_start, line_end) VALUES (?, ?, ?, ?, ?)`,
			fileID, i, c.Text, c.StartLine, c.EndLine)
		if err != nil {
			return fmt.Errorf(i18n.T("store: insert chunk seq=%d: %w", "store: вставка chunk seq=%d: %w"), i, err)
		}
		chunkID, err := res.LastInsertId()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(
			`INSERT INTO fts_chunks(body, path, ids, chunk_id) VALUES (?, ?, ?, ?)`,
			stem.Text(c.Text), stem.Path(path), stem.Idents(c.Text), chunkID,
		); err != nil {
			return fmt.Errorf(i18n.T("store: insert into fts_chunks seq=%d: %w", "store: вставка в fts_chunks seq=%d: %w"), i, err)
		}
		if len(vectors) == 0 {
			continue
		}
		blob, err := vecext.SerializeFloat32(vectors[i])
		if err != nil {
			return fmt.Errorf(i18n.T("store: serialize vector seq=%d: %w", "store: сериализация вектора seq=%d: %w"), i, err)
		}
		if _, err := tx.Exec(`INSERT INTO vec_chunks(chunk_id, embedding) VALUES (?, ?)`, chunkID, blob); err != nil {
			return fmt.Errorf(i18n.T("store: insert vector seq=%d: %w", "store: вставка вектора seq=%d: %w"), i, err)
		}
	}

	return tx.Commit()
}

// Result - один найденный чанк с его расстоянием до запроса.
type Result struct {
	Path     string
	Seq      int
	Text     string
	Distance float64
	// Score - единый показатель релевантности для семантического и
	// лексического поиска: больше значение - тем релевантнее результат.
	Score float64
	// StartLine и EndLine - диапазон строк исходного файла (1-based,
	// EndLine включительно), который покрывает чанк. Для баз, созданных
	// версией senso без этой колонки, тут были бы нули, но такие базы
	// отсекаются проверкой CheckSchema и требуют переиндексации.
	StartLine int
	EndLine   int
}

// Search выполняет KNN-поиск ближайших k чанков к вектору vector. Тай-брейк
// по (path, seq) в ORDER BY делает порядок результатов с одинаковым
// distance детерминированным - без него SQLite не гарантирует стабильный
// порядок строк с равным значением сортировки.
func (s *Store) Search(vector []float32, k int) ([]Result, error) {
	hasVectors, err := s.HasVectors()
	if err != nil {
		return nil, err
	}
	if !hasVectors {
		return nil, errors.New(i18n.T(
			"store: index has no vectors; run senso index --embed",
			"store: в индексе нет векторов; запустите senso index --embed",
		))
	}

	blob, err := vecext.SerializeFloat32(vector)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("store: serialize query vector: %w", "store: сериализация вектора запроса: %w"), err)
	}

	rows, err := s.db.Query(
		`SELECT f.path, c.seq, c.text, v.distance, c.line_start, c.line_end
		 FROM vec_chunks v
		 JOIN chunks c ON c.id = v.chunk_id
		 JOIN files f ON f.id = c.file_id
		 WHERE v.embedding MATCH ? AND k = ?
		 ORDER BY v.distance, f.path, c.seq`,
		blob, k,
	)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("store: KNN search: %w", "store: KNN-поиск: %w"), err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Path, &r.Seq, &r.Text, &r.Distance, &r.StartLine, &r.EndLine); err != nil {
			return nil, err
		}
		// Distance - косинусное расстояние, Score - обратная ему близость.
		r.Score = 1 - r.Distance
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// bm25Weights - веса колонок fts_chunks в порядке их объявления (body,
// path, ids, chunk_id) для функции bm25. Основной вес у текста чанка;
// идентификаторы чуть слабее, потому что их слова уже частично попадают в
// body; путь слабее всех, так как совпадение по пути одинаково для всех
// чанков файла и само по себе не говорит, какой из них релевантнее.
// Последний вес нулевой: chunk_id не индексируется.
const bm25Weights = "1.0, 0.4, 0.8, 0.0"

// SearchLexical выполняет полнотекстовый поиск по fts_chunks и возвращает до
// k наиболее релевантных чанков. Query стеммируется через stem.Query, что
// обеспечивает совпадение словоформ и поддержку фраз/префиксов. Если запрос
// оказался пустым (например, состоял только из пунктуации) или ничего не
// найдено, возвращает пустой срез без ошибки. Тай-брейк по (path, seq) в
// ORDER BY делает порядок результатов с одинаковым rank детерминированным.
func (s *Store) SearchLexical(query string, k int) ([]Result, error) {
	stemmed := stem.Query(query)
	if stemmed == "" {
		return nil, nil
	}

	rows, err := s.db.Query(
		`SELECT f.path, c.seq, c.text, bm25(fts_chunks, `+bm25Weights+`) AS rank,
		        c.line_start, c.line_end
		 FROM fts_chunks
		 JOIN chunks c ON c.id = fts_chunks.chunk_id
		 JOIN files f ON f.id = c.file_id
		 WHERE fts_chunks MATCH ?
		 ORDER BY rank, f.path, c.seq
		 LIMIT ?`,
		stemmed, k,
	)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("store: lexical search: %w", "store: лексический поиск: %w"), err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		var rank float64
		if err := rows.Scan(&r.Path, &r.Seq, &r.Text, &rank, &r.StartLine, &r.EndLine); err != nil {
			return nil, err
		}
		// bm25 в SQLite отрицателен: чем меньше (отрицательнее), тем
		// релевантнее, поэтому Score - это его инверсия.
		r.Score = -rank
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// Chunks возвращает чанки файла path с seq в диапазоне [fromSeq, toSeq]
// включительно, упорядоченные по возрастанию seq. Диапазон, выходящий за
// границы существующих чанков, не ошибка - просто в ответе будет меньше
// элементов. Используется командой "show" для чтения сохранённого текста
// по ссылке из выдачи search.
//
// Возвращает Result, а не отдельный тип: набор нужных полей (Path, Seq,
// Text, StartLine, EndLine) у Result уже есть, а Score/Distance просто
// остаются нулевыми - завести под это единственное отличие новый тип
// означало бы дублировать структуру ради двух неиспользуемых полей.
func (s *Store) Chunks(path string, fromSeq, toSeq int) ([]Result, error) {
	var exists bool
	if err := s.db.QueryRow(`SELECT EXISTS(SELECT 1 FROM files WHERE path=?)`, path).Scan(&exists); err != nil {
		return nil, err
	}
	if !exists {
		return nil, errors.New(i18n.Tf(
			"store: file %q not found in the index",
			"store: файл %q не найден в индексе",
			path,
		))
	}

	rows, err := s.db.Query(
		`SELECT f.path, c.seq, c.text, c.line_start, c.line_end
		 FROM chunks c
		 JOIN files f ON f.id = c.file_id
		 WHERE f.path = ? AND c.seq BETWEEN ? AND ?
		 ORDER BY c.seq`,
		path, fromSeq, toSeq,
	)
	if err != nil {
		return nil, fmt.Errorf(i18n.T("store: read chunks: %w", "store: чтение chunks: %w"), err)
	}
	defer rows.Close()

	var results []Result
	for rows.Next() {
		var r Result
		if err := rows.Scan(&r.Path, &r.Seq, &r.Text, &r.StartLine, &r.EndLine); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// ChunkSeqRange возвращает наименьший и наибольший номера чанков файла
// path. found=false означает, что у файла нет чанков (или самого файла нет
// в индексе). Отдельный запрос вместо чтения всех чанков нужен потому, что
// диапазон требуется только для текста сообщения об ошибке, а у крупных
// файлов чанков сотни и тянуть их текст ради двух чисел незачем.
func (s *Store) ChunkSeqRange(path string) (min, max int, found bool, err error) {
	var lo, hi sql.NullInt64
	err = s.db.QueryRow(
		`SELECT min(c.seq), max(c.seq)
		 FROM chunks c
		 JOIN files f ON f.id = c.file_id
		 WHERE f.path = ?`,
		path,
	).Scan(&lo, &hi)
	if err != nil {
		return 0, 0, false, err
	}
	if !lo.Valid || !hi.Valid {
		return 0, 0, false, nil
	}
	return int(lo.Int64), int(hi.Int64), true, nil
}

// subtreeCondition - условие ограничения путём или поддеревом,
// используется одинаково во всех методах, работающих с деревом путей.
// Вторым аргументом запроса обязан идти subtreePattern(prefix): шаблон LIKE
// собирается в Go, чтобы экранировать метасимволы % и _, которые легальны
// в именах файлов, но иначе трактовались бы LIKE как подстановочные знаки.
const subtreeCondition = `path = ? OR path LIKE ? ESCAPE '\'`

// likeEscaper экранирует метасимволы LIKE (% и _) и сам символ
// экранирования, чтобы они сопоставлялись буквально.
var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// subtreePattern строит шаблон LIKE «всё внутри поддерева prefix».
func subtreePattern(prefix string) string {
	return likeEscaper.Replace(prefix) + "/%"
}

// ListPaths возвращает пути всех проиндексированных файлов, чей путь равен
// prefix или лежит внутри поддерева prefix. Пустой prefix возвращает все пути.
func (s *Store) ListPaths(prefix string) ([]string, error) {
	var rows *sql.Rows
	var err error
	if prefix == "" {
		rows, err = s.db.Query(`SELECT path FROM files ORDER BY path`)
	} else {
		rows, err = s.db.Query(`SELECT path FROM files WHERE `+subtreeCondition+` ORDER BY path`, prefix, subtreePattern(prefix))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		paths = append(paths, p)
	}
	return paths, rows.Err()
}

// FileMeta - сохранённое в индексе состояние файла.
type FileMeta struct {
	MTime int64
	Size  int64
	Hash  string
}

// FileStates возвращает состояние всех проиндексированных файлов поддерева
// prefix одним запросом. Пустой prefix означает всю базу. Нужен проверке
// свежести индекса, где вызов FileState на каждый файл дал бы слишком
// много запросов.
func (s *Store) FileStates(prefix string) (map[string]FileMeta, error) {
	var rows *sql.Rows
	var err error
	if prefix == "" {
		rows, err = s.db.Query(`SELECT path, mtime, size, hash FROM files`)
	} else {
		rows, err = s.db.Query(`SELECT path, mtime, size, hash FROM files WHERE `+subtreeCondition, prefix, subtreePattern(prefix))
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	states := make(map[string]FileMeta)
	for rows.Next() {
		var path string
		var m FileMeta
		if err := rows.Scan(&path, &m.MTime, &m.Size, &m.Hash); err != nil {
			return nil, err
		}
		states[path] = m
	}
	return states, rows.Err()
}

// DeleteFiles удаляет перечисленные файлы вместе с их чанками и векторами.
// Возвращает количество действительно удалённых файлов.
func (s *Store) DeleteFiles(paths []string) (int, error) {
	if len(paths) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	deleted := 0
	for _, path := range paths {
		n, err := deleteFileTx(tx, path)
		if err != nil {
			return 0, err
		}
		deleted += n
	}

	return deleted, tx.Commit()
}

// DeleteSubtree удаляет все файлы, чей путь равен prefix или лежит внутри
// поддерева prefix, вместе с их чанками и векторами. Возвращает количество
// удалённых файлов.
func (s *Store) DeleteSubtree(prefix string) (int, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	rows, err := tx.Query(`SELECT path FROM files WHERE `+subtreeCondition, prefix, subtreePattern(prefix))
	if err != nil {
		return 0, err
	}
	var paths []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			rows.Close()
			return 0, err
		}
		paths = append(paths, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	deleted := 0
	for _, p := range paths {
		n, err := deleteFileTx(tx, p)
		if err != nil {
			return 0, err
		}
		deleted += n
	}

	return deleted, tx.Commit()
}

// deleteFileTx удаляет один файл path вместе с его чанками и векторами в
// рамках уже открытой транзакции tx. Возвращает 1, если файл был найден и
// удалён, 0 - если файла не было.
func deleteFileTx(tx *sql.Tx, path string) (int, error) {
	var fileID int64
	err := tx.QueryRow(`SELECT id FROM files WHERE path=?`, path).Scan(&fileID)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}

	rows, err := tx.Query(`SELECT id FROM chunks WHERE file_id=?`, fileID)
	if err != nil {
		return 0, err
	}
	var chunkIDs []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return 0, err
		}
		chunkIDs = append(chunkIDs, id)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return 0, err
	}
	rows.Close()

	if len(chunkIDs) > 0 {
		var n int
		if err := tx.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='vec_chunks'`).Scan(&n); err != nil {
			return 0, err
		}
		if n > 0 {
			for _, id := range chunkIDs {
				if _, err := tx.Exec(`DELETE FROM vec_chunks WHERE chunk_id=?`, id); err != nil {
					return 0, err
				}
			}
		}
		for _, id := range chunkIDs {
			if _, err := tx.Exec(`DELETE FROM fts_chunks WHERE chunk_id=?`, id); err != nil {
				return 0, err
			}
		}
	}
	if _, err := tx.Exec(`DELETE FROM chunks WHERE file_id=?`, fileID); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(`DELETE FROM files WHERE id=?`, fileID); err != nil {
		return 0, err
	}

	return 1, nil
}

// Stats - агрегированная статистика по индексу.
type Stats struct {
	Files     int
	Chunks    int
	Model     string
	Dim       int
	Root      string
	IndexedAt string
	// Roots - количество файлов по зарегистрированным корням базы
	// (см. AddRoot/Roots); файлы, не попавшие ни в один корень, считаются
	// отдельным ключом.
	Roots map[string]int
	// FTSRows - число строк в лексическом индексе fts_chunks.
	FTSRows int
	// Vectors - число векторов в vec_chunks (0, если таблица ещё не создана).
	Vectors int
}

// Stats возвращает сводную статистику по индексу.
func (s *Store) Stats() (Stats, error) {
	var st Stats

	if err := s.db.QueryRow(`SELECT count(*) FROM files`).Scan(&st.Files); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM chunks`).Scan(&st.Chunks); err != nil {
		return st, err
	}

	model, dim, err := s.Meta()
	if err != nil {
		return st, err
	}
	st.Model, st.Dim = model, dim

	if err := s.db.QueryRow(`SELECT value FROM meta WHERE key='root'`).Scan(&st.Root); err != nil {
		return st, err
	}
	if err := s.db.QueryRow(`SELECT value FROM meta WHERE key='indexed_at'`).Scan(&st.IndexedAt); err != nil {
		return st, err
	}

	st.Roots, err = s.rootCounts()
	if err != nil {
		return st, err
	}

	if err := s.db.QueryRow(`SELECT count(*) FROM fts_chunks`).Scan(&st.FTSRows); err != nil {
		return st, err
	}

	hasVectors, err := s.HasVectors()
	if err != nil {
		return st, err
	}
	if hasVectors {
		if err := s.db.QueryRow(`SELECT count(*) FROM vec_chunks`).Scan(&st.Vectors); err != nil {
			return st, err
		}
	}

	return st, nil
}

// rootCounts группирует количество файлов по зарегистрированным корням
// (meta.roots). Каждый путь относится к самому специфичному (самому
// длинному) из корней, в поддерево которого он попадает - это разруливает
// случай вложенных корней. Пути, не попавшие ни в один зарегистрированный
// корень (возможно у старых баз с неполным meta.roots), группируются под
// отдельным ключом, а не молча приписываются чужому корню.
func (s *Store) rootCounts() (map[string]int, error) {
	roots, err := s.Roots()
	if err != nil {
		return nil, err
	}

	paths, err := s.ListPaths("")
	if err != nil {
		return nil, err
	}

	otherKey := i18n.T("(other)", "(прочее)")

	counts := make(map[string]int)
	for _, p := range paths {
		key, ok := bestRoot(p, roots)
		if !ok {
			key = otherKey
		}
		counts[key]++
	}
	return counts, nil
}

// bestRoot ищет среди roots самый специфичный (самый длинный по строке)
// корень, в поддерево которого входит path. Возвращает false, если ни один
// корень не подошёл.
func bestRoot(path string, roots []string) (string, bool) {
	best := ""
	found := false
	for _, r := range roots {
		if !pathInSubtree(path, r) {
			continue
		}
		if !found || len(r) > len(best) {
			best = r
			found = true
		}
	}
	return best, found
}

// SetIndexedAt записывает время последней успешной индексации в meta.
func (s *Store) SetIndexedAt(t time.Time) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES ('indexed_at', ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, t.Format(time.RFC3339))
	return err
}
