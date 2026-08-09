package store

import (
	"path/filepath"
	"strings"
	"testing"

	"senso/internal/stem"
)

const testDim = 4

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

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", []string{"chunk a"}, [][]float32{{1, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/b.txt", 1, 10, "hashB", []string{"chunk b"}, [][]float32{{0.9, 0.1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/c.txt", 1, 10, "hashC", []string{"chunk c"}, [][]float32{{0, 1, 0, 0}}); err != nil {
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

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hash1", []string{"one", "two"}, [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/a.txt", 2, 20, "hash2", []string{"three"}, [][]float32{{0, 0, 1, 0}}); err != nil {
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

	if err := s.ReplaceFile("/root/a.txt", 42, 100, "abc", []string{"chunk"}, [][]float32{{1, 0, 0, 0}}); err != nil {
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
		if err := s.ReplaceFile(path, 1, 1, "h", []string{"text"}, [][]float32{vec}); err != nil {
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

	if err := s.ReplaceFile("/root/a.txt", 1, 1, "h1", []string{"one", "two"}, [][]float32{{1, 0, 0, 0}, {0, 1, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/b.txt", 1, 1, "h2", []string{"three"}, [][]float32{{0, 0, 1, 0}}); err != nil {
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

	if err := s.EnsureVectors(8); err != nil {
		t.Fatal(err)
	}
	if err := s.EnsureVectors(8); err != nil {
		t.Fatalf("повторный EnsureVectors вернул ошибку: %v", err)
	}

	has, err := s.HasVectors()
	if err != nil {
		t.Fatal(err)
	}
	if !has {
		t.Error("HasVectors() = false после EnsureVectors, ожидалось true")
	}
}

func TestReplaceFileWithoutVectors(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", []string{"chunk a"}, nil); err != nil {
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

	chunks := []string{"локальный поиск по файлам", "второй чанк про индекс"}
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

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hash1", []string{"локальный поиск по файлам"}, [][]float32{{1, 0, 0, 0}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/a.txt", 2, 20, "hash2", []string{"новое содержимое документа", "ещё один чанк"}, [][]float32{{0, 1, 0, 0}, {0, 0, 1, 0}}); err != nil {
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

	if err := s.ReplaceFile("/a/x.txt", 1, 1, "h", []string{"локальный поиск"}, [][]float32{{1, 0, 0, 0}}); err != nil {
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

	if err := s.ReplaceFile("/a/x.txt", 1, 1, "h", []string{"чанк без векторов"}, nil); err != nil {
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

	chunks := []string{
		"локальный поиск по файлам",
		"векторное представление документа",
		"the quick brown fox jumps",
	}
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
		more := []string{"поиск второй раз", "поиск третий раз"}
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

	chunks := []string{"Локальный поиск по файлам проекта"}
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
	if results[0].Text != chunks[0] {
		t.Errorf("SearchLexical(\"файл\") нашёл %q, ожидалось %q", results[0].Text, chunks[0])
	}
}

// TestSearchLexicalPhrase проверяет, что запрос в кавычках ищет точную фразу:
// два чанка содержат оба слова "поиск" и "файлов", но только в первом они
// стоят подряд.
func TestSearchLexicalPhrase(t *testing.T) {
	s := mustOpenInit(t, "", 0)

	match := "Поиском файлов занимается индексатор"
	other := "Локальный поиск по файлам проекта"
	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", []string{match}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceFile("/root/b.txt", 1, 10, "hashB", []string{other}, nil); err != nil {
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

	if err := s.ReplaceFile("/root/a.txt", 1, 10, "hashA", []string{"какой-то текст"}, nil); err != nil {
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
