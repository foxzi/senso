package store

import (
	"path/filepath"
	"strings"
	"testing"

	"senso/internal/chunk"
	"senso/internal/stem"
)

const testDim = 4

// chunksOf строит срез чанков из голых текстов для тестов, которым не важны
// конкретные номера строк: каждому тексту присваивается своя строка (1-based).
func chunksOf(texts ...string) []chunk.Chunk {
	chunks := make([]chunk.Chunk, len(texts))
	for i, text := range texts {
		chunks[i] = chunk.Chunk{Text: text, StartLine: i + 1, EndLine: i + 1}
	}
	return chunks
}

func mustOpenInit(t *testing.T, model string, dim int) *Store {
	t.Helper()
	path := filepath.Join(t.TempDir(), "index.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Init(model, dim, "/root"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInitCreatesSchemaAndIsIdempotent(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	model, dim, err := s.Meta()
	if err != nil {
		t.Fatal(err)
	}
	if model != "bge-m3" || dim != testDim {
		t.Fatalf("Meta() = %q, %d; ожидалось bge-m3, %d", model, dim, testDim)
	}

	// Повторный Init с той же моделью должен пройти без ошибок.
	if err := s.Init("bge-m3", testDim, "/root"); err != nil {
		t.Fatalf("повторный Init с той же моделью вернул ошибку: %v", err)
	}
}

func TestCheckSchemaFreshStoreIsNil(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	// Схема ещё не создана (Init не вызывался) - проверять нечего.
	if err := s.CheckSchema(); err != nil {
		t.Fatalf("CheckSchema() на свежем хранилище вернул ошибку: %v", err)
	}
}

func TestCheckSchemaRejectsOldVersion(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	if err := s.SetMeta("schema_version", "1"); err != nil {
		t.Fatal(err)
	}

	err := s.CheckSchema()
	if err == nil {
		t.Fatal("ожидалась ошибка при несовпадении версии схемы")
	}
	if !strings.Contains(err.Error(), "1") || !strings.Contains(err.Error(), schemaVersion) {
		t.Fatalf("сообщение об ошибке не содержит обе версии: %v", err)
	}
}

func TestInitRejectsDifferentModel(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	err := s.Init("other-model", testDim, "/root")
	if err == nil {
		t.Fatal("ожидалась ошибка при Init с другой моделью")
	}
}

func TestReplaceFileAndSearch(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", chunksOf("chunk a"), [][]float32{{1, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/b.txt", 1, 10, "hashB", chunksOf("chunk b"), [][]float32{{0.9, 0.1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/c.txt", 1, 10, "hashC", chunksOf("chunk c"), [][]float32{{0, 1, 0, 0}}); err != nil {
		t.Fatal(err)
	}

	results, err := s.Search([]float32{1, 0, 0, 0}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 3 {
		t.Fatalf("Search вернул %d результатов, ожидалось 3", len(results))
	}

	wantOrder := []string{"/root/a.txt", "/root/b.txt", "/root/c.txt"}
	for i, r := range results {
		if r.Path != wantOrder[i] {
			t.Errorf("результат %d: путь %q, ожидалось %q", i, r.Path, wantOrder[i])
		}
	}
	for i := 1; i < len(results); i++ {
		if results[i].Distance < results[i-1].Distance {
			t.Errorf("результаты не отсортированы по возрастанию distance: %v", results)
		}
	}
}

func TestReplaceFileReplacesNotDuplicates(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hash1", chunksOf("one", "two"), [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/a.txt", 2, 20, "hash2", chunksOf("three"), [][]float32{{0, 0, 1, 0}}); err != nil {
		t.Fatal(err)
	}

	var chunkCount, vecCount, fileCount int
	if err := s.db.QueryRow(`SELECT count(*) FROM chunks`).Scan(&chunkCount); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM vec_chunks`).Scan(&vecCount); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM files`).Scan(&fileCount); err != nil {
		t.Fatal(err)
	}

	if fileCount != 1 {
		t.Errorf("files count = %d, ожидалось 1", fileCount)
	}
	if chunkCount != 1 {
		t.Errorf("chunks count = %d, ожидалось 1 (замена, не дублирование)", chunkCount)
	}
	if vecCount != 1 {
		t.Errorf("vec_chunks count = %d, ожидалось 1", vecCount)
	}

	mtime, size, hash, found, err := s.FileState("/root/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !found || mtime != 2 || size != 20 || hash != "hash2" {
		t.Errorf("FileState = %d, %d, %q, %v; ожидалось 2, 20, hash2, true", mtime, size, hash, found)
	}
}

func TestFileState(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	_, _, _, found, err := s.FileState("/root/unknown.txt")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Error("FileState для неизвестного пути вернул found=true")
	}

	if err := s.ReplaceFile("/root/a.txt", 42, 100, "abc", chunksOf("chunk"), [][]float32{{1, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}

	mtime, size, hash, found, err := s.FileState("/root/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !found || mtime != 42 || size != 100 || hash != "abc" {
		t.Errorf("FileState = %d, %d, %q, %v; ожидалось 42, 100, abc, true", mtime, size, hash, found)
	}
}

func TestDeleteSubtree(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	files := map[string][]float32{
		"/a/x.txt":   {1, 0, 0, 0},
		"/a/b/y.txt": {0, 1, 0, 0},
		"/c/z.txt":   {0, 0, 1, 0},
	}
	for path, vec := range files {
		if err := s.ReplaceFile(path, 1, 1, "h", chunksOf("text"), [][]float32{vec}); err != nil {
			t.Fatal(err)
		}
	}

	n, err := s.DeleteSubtree("/a")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("DeleteSubtree удалил %d файлов, ожидалось 2", n)
	}

	paths, err := s.ListPaths("")
	if err != nil {
		t.Fatal(err)
	}
	if len(paths) != 1 || paths[0] != "/c/z.txt" {
		t.Fatalf("ListPaths = %v, ожидалось [/c/z.txt]", paths)
	}

	var chunkCount, vecCount int
	if err := s.db.QueryRow(`SELECT count(*) FROM chunks`).Scan(&chunkCount); err != nil {
		t.Fatal(err)
	}
	if err := s.db.QueryRow(`SELECT count(*) FROM vec_chunks`).Scan(&vecCount); err != nil {
		t.Fatal(err)
	}
	if chunkCount != 1 || vecCount != 1 {
		t.Errorf("после DeleteSubtree остались осиротевшие записи: chunks=%d, vec_chunks=%d", chunkCount, vecCount)
	}
}

func TestStats(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	if err := s.ReplaceFile("/root/a.txt", 1, 1, "h1", chunksOf("one", "two"), [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/b.txt", 1, 1, "h2", chunksOf("three"), [][]float32{{0, 0, 1, 0}}); err != nil {
		t.Fatal(err)
	}

	st, err := s.Stats()
	if err != nil {
		t.Fatal(err)
	}
	if st.Files != 2 {
		t.Errorf("Stats.Files = %d, ожидалось 2", st.Files)
	}
	if st.Chunks != 3 {
		t.Errorf("Stats.Chunks = %d, ожидалось 3", st.Chunks)
	}
	if st.Model != "bge-m3" || st.Dim != testDim {
		t.Errorf("Stats.Model/Dim = %q/%d, ожидалось bge-m3/%d", st.Model, st.Dim, testDim)
	}
}

func TestInitLexicalOnly(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	has, err := s.HasVectors()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("HasVectors() = true на свежем лексическом индексе, ожидалось false")
	}

	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='fts_chunks'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Error("таблица fts_chunks не создана схемой")
	}
}

func TestSetMetaGetMeta(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	v, err := s.GetMeta("query_prefix")
	if err != nil {
		t.Fatal(err)
	}
	if v != "" {
		t.Errorf("GetMeta для отсутствующего ключа = %q, ожидалась пустая строка", v)
	}

	if err := s.SetMeta("query_prefix", "query: "); err != nil {
		t.Fatal(err)
	}
	v, err = s.GetMeta("query_prefix")
	if err != nil {
		t.Fatal(err)
	}
	if v != "query: " {
		t.Errorf("GetMeta после SetMeta = %q, ожидалось %q", v, "query: ")
	}

	// Повторный SetMeta с тем же ключом должен перезаписать значение.
	if err := s.SetMeta("query_prefix", "новый префикс"); err != nil {
		t.Fatal(err)
	}
	v, err = s.GetMeta("query_prefix")
	if err != nil {
		t.Fatal(err)
	}
	if v != "новый префикс" {
		t.Errorf("GetMeta после повторного SetMeta = %q, ожидалось %q", v, "новый префикс")
	}
}

func TestEnsureVectorsIdempotent(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := ensureVectorsExec(s.db, 8); err != nil {
		t.Fatal(err)
	}
	if err := ensureVectorsExec(s.db, 8); err != nil {
		t.Fatalf("повторный ensureVectorsExec вернул ошибку: %v", err)
	}

	has, err := s.HasVectors()
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("HasVectors() = false после ensureVectorsExec, ожидалось true")
	}
}

func TestReplaceFileWithoutVectors(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", chunksOf("chunk a"), nil); err != nil {
		t.Fatal(err)
	}

	has, err := s.HasVectors()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Error("HasVectors() = true после ReplaceFile без векторов, ожидалось false")
	}

	mtime, size, hash, found, err := s.FileState("/root/a.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !found || mtime != 1 || size != 10 || hash != "hashA" {
		t.Errorf("FileState = %d, %d, %q, %v; ожидалось 1, 10, hashA, true", mtime, size, hash, found)
	}
}

// ftsMatchCount возвращает число строк fts_chunks, найденных по MATCH-запросу
// query. В fts_chunks хранятся стеммы, поэтому query тоже стеммируется через
// stem.Text перед сравнением.
func ftsMatchCount(t *testing.T, s *Store, query string) int {
	t.Helper()
	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM fts_chunks WHERE fts_chunks MATCH ?`, stem.Text(query)).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestReplaceFilePopulatesFTS(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	chunks := chunksOf("локальный поиск по файлам", "второй чанк про индекс")
	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hash1", chunks, [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}}); err != nil {
		t.Fatal(err)
	}

	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM fts_chunks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("fts_chunks count = %d, ожидалось 2", n)
	}

	if ftsMatchCount(t, s, "локальный") != 1 {
		t.Error("MATCH 'локальный' не нашёл вставленный чанк")
	}
}

func TestReplaceFileReindexUpdatesFTS(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hash1", chunksOf("локальный поиск по файлам"), [][]float32{{1, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/a.txt", 2, 20, "hash2", chunksOf("новое содержимое документа", "ещё один чанк"), [][]float32{{0, 1, 0, 0}, {0, 0, 1, 0}}); err != nil {
		t.Fatal(err)
	}

	if ftsMatchCount(t, s, "локальный") != 0 {
		t.Error("старое слово всё ещё находится в fts_chunks после переиндексации")
	}
	if ftsMatchCount(t, s, "содержимое") != 1 {
		t.Error("новое слово не находится в fts_chunks после переиндексации")
	}

	var n int
	if err := s.db.QueryRow(`SELECT count(*) FROM fts_chunks`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("fts_chunks count = %d, ожидалось 2 после переиндексации", n)
	}
}

func TestDeleteSubtreeCleansFTS(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	if err := s.ReplaceFile("/a/x.txt", 1, 1, "h", chunksOf("локальный поиск"), [][]float32{{1, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}

	n, err := s.DeleteSubtree("/a")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("DeleteSubtree удалил %d файлов, ожидалось 1", n)
	}

	var count int
	if err := s.db.QueryRow(`SELECT count(*) FROM fts_chunks`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Errorf("fts_chunks count = %d после DeleteSubtree, ожидалось 0", count)
	}
}

func TestDeleteSubtreeWithoutVectorsTable(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/a/x.txt", 1, 1, "h", chunksOf("чанк без векторов"), nil); err != nil {
		t.Fatal(err)
	}

	has, err := s.HasVectors()
	if err != nil {
		t.Fatal(err)
	}
	if has {
		t.Fatal("HasVectors() = true, ожидалось false для чисто лексической базы")
	}

	n, err := s.DeleteSubtree("/a")
	if err != nil {
		t.Fatalf("DeleteSubtree на базе без vec_chunks вернул ошибку: %v", err)
	}
	if n != 1 {
		t.Fatalf("DeleteSubtree удалил %d файлов, ожидалось 1", n)
	}
}

func TestSearchLexical(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	chunks := chunksOf(
		"локальный поиск по файлам",
		"векторное представление документа",
		"the quick brown fox jumps",
	)
	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", chunks, nil); err != nil {
		t.Fatal(err)
	}

	assertFindsChunk := func(t *testing.T, query string) {
		t.Helper()
		results, err := s.SearchLexical(query, 10)
		if err != nil {
			t.Fatalf("SearchLexical(%q) вернул ошибку: %v", query, err)
		}
		if len(results) == 0 {
			t.Fatalf("SearchLexical(%q) не нашёл ни одного результата", query)
		}
		found := false
		for _, r := range results {
			if r.Text == "локальный поиск по файлам" {
				found = true
				if r.Score <= 0 {
					t.Errorf("Score = %v, ожидалось положительное значение", r.Score)
				}
				if r.Distance != 0 {
					t.Errorf("Distance = %v, ожидалось 0 для лексического поиска", r.Distance)
				}
			}
		}
		if !found {
			t.Errorf("SearchLexical(%q) не нашёл ожидаемый чанк", query)
		}
	}

	t.Run("русское слово", func(t *testing.T) {
		assertFindsChunk(t, "поиск")
	})
	t.Run("регистронезависимость", func(t *testing.T) {
		assertFindsChunk(t, "ПОИСК")
	})
	t.Run("префиксный поиск", func(t *testing.T) {
		assertFindsChunk(t, "поис*")
	})

	t.Run("спецсимволы не приводят к ошибке", func(t *testing.T) {
		results, err := s.SearchLexical("поиск (файлам)", 10)
		if err != nil {
			t.Fatalf("SearchLexical со спецсимволами вернул ошибку: %v", err)
		}
		if len(results) == 0 {
			t.Error("SearchLexical со спецсимволами не нашёл ожидаемый чанк")
		}
	})

	t.Run("ничего не найдено", func(t *testing.T) {
		results, err := s.SearchLexical("несуществующееслово", 10)
		if err != nil {
			t.Fatalf("SearchLexical вернул ошибку: %v", err)
		}
		if len(results) != 0 {
			t.Errorf("SearchLexical нашёл %d результатов, ожидалось 0", len(results))
		}
	})

	t.Run("k ограничивает число результатов", func(t *testing.T) {
		// Добавляем ещё чанки со словом "поиск", чтобы запрос находил
		// несколько результатов и можно было проверить действие LIMIT.
		more := chunksOf("поиск второй раз", "поиск третий раз")
		if err := s.ReplaceFile("/root/b.txt", 1, 10, "hashB", more, nil); err != nil {
			t.Fatal(err)
		}

		results, err := s.SearchLexical("поиск", 1)
		if err != nil {
			t.Fatalf("SearchLexical вернул ошибку: %v", err)
		}
		if len(results) != 1 {
			t.Errorf("SearchLexical с k=1 вернул %d результатов, ожидалось 1", len(results))
		}
	})
}

// TestSearchLexicalStemsWordForms проверяет, что поиск находит другую
// словоформу слова благодаря стеммингу: запрос "файл" должен найти чанк со
// словом "файлам".
func TestSearchLexicalStemsWordForms(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	chunks := chunksOf("Локальный поиск по файлам проекта")
	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", chunks, nil); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchLexical("файл", 10)
	if err != nil {
		t.Fatalf("SearchLexical вернул ошибку: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchLexical(\"файл\") вернул %d результатов, ожидалось 1", len(results))
	}
	if results[0].Text != chunks[0].Text {
		t.Errorf("SearchLexical(\"файл\") нашёл %q, ожидалось %q", results[0].Text, chunks[0].Text)
	}
}

// TestSearchLexicalPhrase проверяет, что запрос в кавычках ищет точную фразу:
// два чанка содержат оба слова "поиск" и "файлов", но только в первом они
// стоят подряд.
func TestSearchLexicalPhrase(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	match := "Поиском файлов занимается индексатор"
	other := "Локальный поиск по файлам проекта"
	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", chunksOf(match), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/b.txt", 1, 10, "hashB", chunksOf(other), nil); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchLexical(`"поиск файлов"`, 10)
	if err != nil {
		t.Fatalf("SearchLexical вернул ошибку: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchLexical(фраза) вернул %d результатов, ожидалось 1", len(results))
	}
	if results[0].Text != match {
		t.Errorf("SearchLexical(фраза) нашёл %q, ожидалось %q", results[0].Text, match)
	}
}

// TestSearchLexicalPunctuationOnly проверяет, что запрос из одной пунктуации
// не приводит к ошибке SQL с пустым MATCH, а возвращает пустой результат.
func TestSearchLexicalPunctuationOnly(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", chunksOf("какой-то текст"), nil); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchLexical("!!!", 10)
	if err != nil {
		t.Fatalf("SearchLexical(\"!!!\") вернул ошибку: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("SearchLexical(\"!!!\") вернул %d результатов, ожидалось 0", len(results))
	}
}

// TestRemoveRootDropsCoveredRoots проверяет, что RemoveRoot убирает корень,
// совпадающий с удаляемым путём, и вложенные в него корни.
func TestRemoveRootDropsCoveredRoots(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	for _, r := range []string{"/work/docs", "/work/config", "/work/config/nested"} {
		if err := s.AddRoot(r); err != nil {
			t.Fatalf("AddRoot(%q): %v", r, err)
		}
	}

	changed, err := s.RemoveRoot("/work/config")
	if err != nil {
		t.Fatalf("RemoveRoot: %v", err)
	}
	if !changed {
		t.Fatal("RemoveRoot вернул changed=false, хотя корень был в списке")
	}

	roots, err := s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	for _, r := range roots {
		if strings.HasPrefix(r, "/work/config") {
			t.Fatalf("Roots = %v, корень /work/config не удалён", roots)
		}
	}
	if !containsRoot(roots, "/work/docs") {
		t.Fatalf("Roots = %v, потерян несвязанный корень /work/docs", roots)
	}
}

// TestRemoveRootKeepsAncestor проверяет, что удаление подкаталога не
// выбрасывает корень, внутри которого он лежит: из корня удалили лишь часть
// поддерева, сам он остаётся проиндексированным.
func TestRemoveRootKeepsAncestor(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	if err := s.AddRoot("/work/docs"); err != nil {
		t.Fatalf("AddRoot: %v", err)
	}

	changed, err := s.RemoveRoot("/work/docs/orders")
	if err != nil {
		t.Fatalf("RemoveRoot: %v", err)
	}
	if changed {
		t.Error("RemoveRoot вернул changed=true для подкаталога корня")
	}

	roots, err := s.Roots()
	if err != nil {
		t.Fatalf("Roots: %v", err)
	}
	if !containsRoot(roots, "/work/docs") {
		t.Fatalf("Roots = %v, корень-предок должен сохраниться", roots)
	}
}

// containsRoot сообщает, есть ли want в списке roots.
func containsRoot(roots []string, want string) bool {
	for _, r := range roots {
		if r == want {
			return true
		}
	}
	return false
}

// TestSearchLexicalReturnsLineRange проверяет, что StartLine/EndLine чанка
// доходят до Result в неизменном виде.
func TestSearchLexicalReturnsLineRange(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	chunks := []chunk.Chunk{
		{Text: "первый чанк без совпадения", StartLine: 1, EndLine: 3},
		{Text: "локальный поиск по файлам", StartLine: 4, EndLine: 9},
	}
	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", chunks, nil); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchLexical("поиск", 10)
	if err != nil {
		t.Fatalf("SearchLexical вернул ошибку: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchLexical вернул %d результатов, ожидалось 1", len(results))
	}
	if results[0].StartLine != 4 || results[0].EndLine != 9 {
		t.Errorf("StartLine/EndLine = %d/%d, ожидалось 4/9", results[0].StartLine, results[0].EndLine)
	}
}

// TestSearchLexicalByPath проверяет, что файл находится по словам своего
// пути, даже если в тексте чанка этих слов нет.
func TestSearchLexicalByPath(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/internal/store/migrations.sql", 1, 10, "hashA",
		chunksOf("создание таблиц"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/README.md", 1, 10, "hashB",
		chunksOf("описание проекта"), nil); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchLexical("migrations", 10)
	if err != nil {
		t.Fatalf("SearchLexical вернул ошибку: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SearchLexical(\"migrations\") вернул %d результатов, ожидался 1", len(results))
	}
	if results[0].Path != "/root/internal/store/migrations.sql" {
		t.Errorf("SearchLexical нашёл %q, ожидался файл миграций", results[0].Path)
	}
}

// TestSearchLexicalByIdentifierParts проверяет, что составной идентификатор
// находится по любой записи: по частям, слитно и в другом стиле.
func TestSearchLexicalByIdentifierParts(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	text := "func (s *Store) ReplaceFile(path string) error"
	if err := s.ReplaceFile("/root/a.go", 1, 10, "hashA", chunksOf(text), nil); err != nil {
		t.Fatal(err)
	}

	for _, query := range []string{"ReplaceFile", "replace_file", "replace file"} {
		t.Run(query, func(t *testing.T) {
			results, err := s.SearchLexical(query, 10)
			if err != nil {
				t.Fatalf("SearchLexical вернул ошибку: %v", err)
			}
			if len(results) != 1 {
				t.Fatalf("SearchLexical(%q) вернул %d результатов, ожидался 1", query, len(results))
			}
		})
	}
}

// TestSearchLexicalBodyOutranksPath проверяет расстановку весов: чанк, где
// слово встречается в тексте, релевантнее чанка, где оно есть только в пути.
func TestSearchLexicalBodyOutranksPath(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/notes.txt", 1, 10, "hashA",
		chunksOf("здесь описан индексатор и его устройство"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/индексатор/readme.txt", 1, 10, "hashB",
		chunksOf("посторонний текст"), nil); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchLexical("индексатор", 10)
	if err != nil {
		t.Fatalf("SearchLexical вернул ошибку: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("SearchLexical вернул %d результатов, ожидалось 2", len(results))
	}
	if results[0].Path != "/root/notes.txt" {
		t.Errorf("первым идёт %q, ожидалось совпадение в тексте (/root/notes.txt)", results[0].Path)
	}
}

// TestSearchLexicalPhraseIgnoresPath проверяет, что путь и идентификаторы не
// вклиниваются во фразовый поиск по тексту: фраза из слова пути и слова
// текста не совпадает ни с чем.
func TestSearchLexicalPhraseIgnoresPath(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/отчет.txt", 1, 10, "hashA",
		chunksOf("продажи выросли"), nil); err != nil {
		t.Fatal(err)
	}

	results, err := s.SearchLexical(`"отчет продажи"`, 10)
	if err != nil {
		t.Fatalf("SearchLexical вернул ошибку: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("SearchLexical(фраза через путь) вернул %d результатов, ожидалось 0", len(results))
	}
}

// TestChunksRange проверяет, что Chunks возвращает ровно чанки из заданного
// диапазона seq, упорядоченные по возрастанию, независимо от порядка их
// физической записи.
func TestChunksRange(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/doc.txt", 1, 10, "hash",
		chunksOf("chunk 0", "chunk 1", "chunk 2", "chunk 3", "chunk 4"), nil); err != nil {
		t.Fatal(err)
	}

	got, err := s.Chunks("/root/doc.txt", 1, 3)
	if err != nil {
		t.Fatalf("Chunks вернул ошибку: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("Chunks вернул %d чанков, ожидалось 3", len(got))
	}
	for i, want := range []struct {
		seq  int
		text string
	}{{1, "chunk 1"}, {2, "chunk 2"}, {3, "chunk 3"}} {
		if got[i].Seq != want.seq || got[i].Text != want.text {
			t.Errorf("chunk %d = (seq=%d, text=%q), ожидалось (seq=%d, text=%q)", i, got[i].Seq, got[i].Text, want.seq, want.text)
		}
	}
}

// TestChunksOutOfRangeIsNotAnError проверяет, что диапазон, выходящий за
// границы существующих чанков, просто даёт меньше элементов, а не ошибку.
func TestChunksOutOfRangeIsNotAnError(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/doc.txt", 1, 10, "hash", chunksOf("chunk 0", "chunk 1"), nil); err != nil {
		t.Fatal(err)
	}

	got, err := s.Chunks("/root/doc.txt", -5, 100)
	if err != nil {
		t.Fatalf("Chunks вернул ошибку: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Chunks вернул %d чанков, ожидалось 2 (весь файл)", len(got))
	}

	got, err = s.Chunks("/root/doc.txt", 5, 10)
	if err != nil {
		t.Fatalf("Chunks вернул ошибку: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Chunks вернул %d чанков для диапазона за пределами файла, ожидалось 0", len(got))
	}
}

// TestChunksLineRange проверяет, что line_start/line_end чанков доходят
// до вызывающего кода без изменений.
func TestChunksLineRange(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/doc.txt", 1, 10, "hash", chunksOf("a", "b"), nil); err != nil {
		t.Fatal(err)
	}

	got, err := s.Chunks("/root/doc.txt", 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].StartLine != 1 || got[0].EndLine != 1 {
		t.Errorf("chunk 0: StartLine=%d EndLine=%d, ожидалось 1, 1", got[0].StartLine, got[0].EndLine)
	}
	if got[1].StartLine != 2 || got[1].EndLine != 2 {
		t.Errorf("chunk 1: StartLine=%d EndLine=%d, ожидалось 2, 2", got[1].StartLine, got[1].EndLine)
	}
}

// TestChunksUnknownFile проверяет, что Chunks возвращает понятную ошибку
// для файла, которого нет в индексе.
func TestChunksUnknownFile(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	_, err := s.Chunks("/root/missing.txt", 0, 10)
	if err == nil {
		t.Fatal("Chunks(несуществующий файл) не вернул ошибку")
	}
}

func TestFileStatesReturnsSavedMetadata(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/a.txt", 111, 5, "h1", chunksOf("alpha"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/sub/b.txt", 222, 7, "h2", chunksOf("beta"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/other/c.txt", 333, 9, "h3", chunksOf("gamma"), nil); err != nil {
		t.Fatal(err)
	}

	all, err := s.FileStates("")
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 3 {
		t.Errorf("FileStates(\"\") returned %d files, want 3", len(all))
	}
	want := FileMeta{MTime: 111, Size: 5, Hash: "h1"}
	if all["/root/a.txt"] != want {
		t.Errorf("state = %+v, want %+v", all["/root/a.txt"], want)
	}

	// Поддерево ограничивает выборку по границам сегментов пути.
	sub, err := s.FileStates("/root")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 2 {
		t.Errorf("FileStates(\"/root\") returned %d files, want 2", len(sub))
	}
	if _, ok := sub["/other/c.txt"]; ok {
		t.Error("FileStates must not return files outside the subtree")
	}
}

func TestOpenReadOnlyRejectsWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "index.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Init("", 0, "/root"); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/a.txt", 1, 1, "h", chunksOf("alpha"), nil); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	ro, err := OpenReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()

	if err := ro.CheckSchema(); err != nil {
		t.Errorf("CheckSchema on a read-only store: %v", err)
	}
	states, err := ro.FileStates("")
	if err != nil {
		t.Fatalf("FileStates on a read-only store: %v", err)
	}
	if len(states) != 1 {
		t.Errorf("FileStates returned %d files, want 1", len(states))
	}
	if _, err := ro.DeleteFiles([]string{"/root/a.txt"}); err == nil {
		t.Error("a read-only store must reject writes")
	}
}

func TestOpenReadOnlyMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nope.db")
	s, err := OpenReadOnly(path)
	if err == nil {
		s.Close()
		t.Fatal("OpenReadOnly must fail when the database file does not exist")
	}
}

func TestSubtreeLikeMetacharsAreLiteral(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	// a_b и a%b содержат метасимволы LIKE; axb и aXb совпали бы с ними,
	// если бы шаблон поддерева не экранировался.
	paths := []string{"/r/a_b/f.txt", "/r/axb/f.txt", "/r/a%b/f.txt", "/r/aXb/f.txt"}
	for _, p := range paths {
		if err := s.ReplaceFile(p, 1, 1, "h", chunksOf("text"), nil); err != nil {
			t.Fatal(err)
		}
	}

	got, err := s.ListPaths("/r/a_b")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "/r/a_b/f.txt" {
		t.Fatalf("ListPaths(/r/a_b) = %v, ожидалось [/r/a_b/f.txt]", got)
	}

	states, err := s.FileStates("/r/a%b")
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 {
		t.Fatalf("FileStates(/r/a%%b) вернул %d файлов, ожидался 1: %v", len(states), states)
	}
	if _, ok := states["/r/a%b/f.txt"]; !ok {
		t.Fatalf("FileStates(/r/a%%b) не содержит /r/a%%b/f.txt: %v", states)
	}

	n, err := s.DeleteSubtree("/r/a_b")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("DeleteSubtree(/r/a_b) удалил %d файлов, ожидался 1", n)
	}
	rest, err := s.ListPaths("")
	if err != nil {
		t.Fatal(err)
	}
	if len(rest) != 3 {
		t.Fatalf("после DeleteSubtree осталось %d файлов, ожидалось 3: %v", len(rest), rest)
	}
}

func TestMetaRejectsGarbageDim(t *testing.T) {
	s := mustOpenInit(t, "bge-m3", testDim)

	if err := s.SetMeta("dim", "10garbage"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Meta(); err == nil {
		t.Fatal("Meta() не вернул ошибку для dim=10garbage")
	}
}
