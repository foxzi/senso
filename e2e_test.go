package main

import (
	"archive/zip"
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

// Сквозные тесты CLI: каждый тест проходит те же шаги, что и вызывающий
// senso агент или скрипт, через run() - от разбора аргументов до вывода.
// Вспомогательный код (дерево файлов, разбор JSON, подставная Ollama)
// лежит в e2ehelpers_test.go.

// statusJSONOut - подмножество полей status --json.
type statusJSONOut struct {
	Mode    string         `json:"mode"`
	Model   string         `json:"model"`
	Dim     int            `json:"dim"`
	Files   int            `json:"files"`
	Chunks  int            `json:"chunks"`
	Vectors int            `json:"vectors"`
	Roots   map[string]int `json:"roots"`
}

// showJSONOut - подмножество полей show --json.
type showJSONOut struct {
	Ref         string `json:"ref"`
	Path        string `json:"path"`
	Chunk       int    `json:"chunk"`
	Stale       bool   `json:"stale"`
	StaleReason string `json:"stale_reason"`
	Chunks      []struct {
		Ref       string `json:"ref"`
		Chunk     int    `json:"chunk"`
		StartLine int    `json:"line_start"`
		Text      string `json:"text"`
	} `json:"chunks"`
}

// sampleTree - небольшое дерево, на котором проверяется основной цикл.
func sampleTree() map[string]string {
	return map[string]string{
		"docs/guide.md": "# Индексация\n\nКоманда index обходит дерево каталогов и сохраняет чанки.\n" +
			"\n## Поиск\n\nЛексический поиск идёт по FTS5 с ранжированием bm25.\n",
		"internal/store.go": "package store\n\n// ReplaceFile заменяет чанки файла в индексе.\nfunc ReplaceFile() {}\n",
		"notes.txt":         "Заметки о хранилище: индекс лежит в скрытом каталоге.\n",
	}
}

// TestE2EFullCycle проходит полный цикл работы с индексом:
// index -> search -> show -> status -> check -> rm.
func TestE2EFullCycle(t *testing.T) {
	root := writeTree(t, sampleTree())
	dbPath := dbIn(t)

	// index: новое дерево целиком попадает в индекс.
	rep := runIndexReport(t, dbPath, root)
	if rep.Scanned != 3 || rep.Indexed != 3 || rep.Updated != 0 {
		t.Errorf("отчёт index = %+v, ожидалось scanned=3 indexed=3 updated=0", rep)
	}
	if rep.Chunks == 0 {
		t.Error("index не записал ни одного чанка")
	}
	if rep.Vectors {
		t.Error("index без --embed не должен сообщать о векторах")
	}
	if len(rep.Failed) != 0 {
		t.Errorf("failed = %+v, ожидался пустой список", rep.Failed)
	}

	// search: находится файл, где встречается запрос.
	resp := runSearchV2(t, dbPath, "ReplaceFile")
	if len(resp.Results) == 0 {
		t.Fatal("search не нашёл ничего по запросу ReplaceFile")
	}
	if resp.Mode != "lexical" || resp.Results[0].ScoreKind != "bm25" {
		t.Errorf("mode = %q, score_kind = %q, ожидались lexical и bm25",
			resp.Mode, resp.Results[0].ScoreKind)
	}
	if got := filepath.Base(resp.Results[0].Path); got != "store.go" {
		t.Errorf("первый результат = %q, ожидался store.go", got)
	}
	if resp.Results[0].Rank != 1 {
		t.Errorf("rank первого результата = %d, ожидался 1", resp.Results[0].Rank)
	}
	if len(resp.Warnings) != 0 {
		t.Errorf("warnings = %+v, для свежего индекса ожидался пустой список", resp.Warnings)
	}

	// show: ссылка из выдачи search читается без изменений.
	ref := resp.Results[0].Ref
	stdout, _ := mustRun(t, "show", "--json", "--db", dbPath, ref)
	var shown showJSONOut
	decodeJSON(t, stdout, &shown)
	if shown.Ref != ref {
		t.Errorf("show вернул ref %q, запрашивался %q", shown.Ref, ref)
	}
	if shown.Stale {
		t.Errorf("show сообщил об устаревшем файле: %s", shown.StaleReason)
	}
	if len(shown.Chunks) != 1 || !strings.Contains(shown.Chunks[0].Text, "ReplaceFile") {
		t.Errorf("show не вернул текст чанка: %+v", shown.Chunks)
	}

	// status: статистика соответствует проиндексированному дереву.
	stdout, _ = mustRun(t, "status", "--json", "--db", dbPath)
	var st statusJSONOut
	decodeJSON(t, stdout, &st)
	if st.Files != 3 || st.Chunks != rep.Chunks {
		t.Errorf("status: files = %d, chunks = %d, ожидались 3 и %d", st.Files, st.Chunks, rep.Chunks)
	}
	if st.Mode != "lexical" || st.Vectors != 0 {
		t.Errorf("status: mode = %q, vectors = %d, ожидались lexical и 0", st.Mode, st.Vectors)
	}
	if st.Roots[root] != 3 {
		t.Errorf("status: roots = %v, ожидался %s с 3 файлами", st.Roots, root)
	}

	// check: сразу после индексации индекс свежий.
	code, chk := runCheckJSON(t, dbPath, root)
	if code != exitOK || !chk.Fresh {
		t.Errorf("check: код = %d, fresh = %v, ожидались 0 и true (%+v)", code, chk.Fresh, chk)
	}
	if chk.Mode != "mtime" || chk.Unchanged != 3 {
		t.Errorf("check: mode = %q, unchanged = %d, ожидались mtime и 3", chk.Mode, chk.Unchanged)
	}

	// rm: запись уходит из индекса, файл на диске остаётся.
	target := filepath.Join(root, "internal")
	mustRun(t, "rm", "--db", dbPath, target)
	if _, err := os.Stat(filepath.Join(target, "store.go")); err != nil {
		t.Errorf("rm удалил файл с диска: %v", err)
	}
	if resp := runSearchV2(t, dbPath, "ReplaceFile"); len(resp.Results) != 0 {
		t.Errorf("после rm search всё ещё находит %v", resp.names())
	}
	stdout, _ = mustRun(t, "status", "--json", "--db", dbPath)
	decodeJSON(t, stdout, &st)
	if st.Files != 2 {
		t.Errorf("после rm status: files = %d, ожидалось 2", st.Files)
	}

	// check после rm: удалённый из индекса файл виден как непроиндексированный.
	code, chk = runCheckJSON(t, dbPath, root)
	if code != exitStale || chk.Fresh {
		t.Errorf("check после rm: код = %d, fresh = %v, ожидались %d и false", code, chk.Fresh, exitStale)
	}
	if chk.Unindexed != 1 {
		t.Errorf("check после rm: unindexed = %d, ожидался 1 (%+v)", chk.Unindexed, chk)
	}
}

// TestE2EReindex проверяет инкрементальную переиндексацию: изменённый,
// новый, неизменившийся и удалённый файлы учитываются по отдельности.
func TestE2EReindex(t *testing.T) {
	root := writeTree(t, sampleTree())
	dbPath := dbIn(t)
	runIndexReport(t, dbPath, root)

	// Повторный запуск без изменений: всё содержимое уже в индексе.
	rep := runIndexReport(t, dbPath, root)
	if rep.Unchanged != 3 || rep.Indexed != 0 || rep.Updated != 0 || rep.Chunks != 0 {
		t.Errorf("повторный index = %+v, ожидалось unchanged=3 без записей", rep)
	}

	// Изменение содержимого, новый файл и удаление старого.
	writeFileIn(t, root, "notes.txt", "Заметки переписаны: теперь про кальмаров.\n")
	writeFileIn(t, root, "docs/api.md", "# API\n\nОписание команды search.\n")
	if err := os.Remove(filepath.Join(root, "internal", "store.go")); err != nil {
		t.Fatal(err)
	}

	rep = runIndexReport(t, dbPath, root)
	if rep.Updated != 1 || rep.Indexed != 1 || rep.Unchanged != 1 || rep.Deleted != 1 {
		t.Errorf("index после изменений = %+v, ожидалось updated=1 indexed=1 unchanged=1 deleted=1", rep)
	}

	// Старое содержимое изменённого файла больше не находится, новое находится.
	if resp := runSearchV2(t, dbPath, "хранилище"); len(resp.Results) != 0 {
		t.Errorf("после переиндексации найдено старое содержимое: %v", resp.names())
	}
	if resp := runSearchV2(t, dbPath, "кальмары"); len(resp.Results) == 0 {
		t.Error("новое содержимое изменённого файла не находится")
	}
	if resp := runSearchV2(t, dbPath, "search"); !slices.Contains(resp.names(), "api.md") {
		t.Error("новый файл api.md не находится после переиндексации")
	}

	code, chk := runCheckJSON(t, dbPath, root)
	if code != exitOK || !chk.Fresh {
		t.Errorf("check после переиндексации: код = %d, fresh = %v (%+v)", code, chk.Fresh, chk)
	}
}

// TestE2EStaleFileWarning проверяет, что изменённый после индексации файл
// отмечается предупреждением в выдаче и признаком stale в show.
func TestE2EStaleFileWarning(t *testing.T) {
	root := writeTree(t, sampleTree())
	dbPath := dbIn(t)
	runIndexReport(t, dbPath, root)

	writeFileIn(t, root, "notes.txt", "Заметки о хранилище: файл переписан после индексации.\n")

	resp := runSearchV2(t, dbPath, "хранилище")
	if len(resp.Results) == 0 {
		t.Fatal("search ничего не нашёл")
	}
	if len(resp.Warnings) != 1 || resp.Warnings[0].Code != "file_modified" {
		t.Fatalf("warnings = %+v, ожидалось одно file_modified", resp.Warnings)
	}
	if filepath.Base(resp.Warnings[0].Path) != "notes.txt" {
		t.Errorf("предупреждение о %q, ожидался notes.txt", resp.Warnings[0].Path)
	}

	stdout, _ := mustRun(t, "show", "--json", "--db", dbPath, resp.Results[0].Ref)
	var shown showJSONOut
	decodeJSON(t, stdout, &shown)
	if !shown.Stale || shown.StaleReason != "modified" {
		t.Errorf("show: stale = %v, reason = %q, ожидались true и modified", shown.Stale, shown.StaleReason)
	}
}

// TestE2ETextOutputStreams фиксирует распределение вывода по потокам:
// результат команды идёт в stdout, прогресс и диагностика - в stderr.
func TestE2ETextOutputStreams(t *testing.T) {
	root := writeTree(t, sampleTree())
	dbPath := dbIn(t)

	// index печатает сводку в stderr, оставляя stdout свободным для отчёта.
	stdout, stderr := mustRun(t, "index", "--db", dbPath, root)
	if stdout != "" {
		t.Errorf("index: stdout = %q, ожидался пустой вывод", stdout)
	}
	if stderr == "" {
		t.Error("index: stderr пуст, ожидалась сводка")
	}

	stdout, stderr = mustRun(t, "index", "--quiet", "--report-json", "--db", dbPath, root)
	if !strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		t.Errorf("index --report-json: stdout = %q, ожидался JSON", stdout)
	}
	if stderr != "" {
		t.Errorf("index --quiet: stderr = %q, ожидался пустой вывод", stderr)
	}

	// search: текстовая выдача и список путей идут в stdout.
	stdout, stderr = mustRun(t, "search", "--db", dbPath, "bm25")
	if !strings.Contains(stdout, "guide.md") {
		t.Errorf("search: stdout = %q, ожидалось упоминание guide.md", stdout)
	}
	if stderr != "" {
		t.Errorf("search: stderr = %q, ожидался пустой вывод", stderr)
	}

	stdout, _ = mustRun(t, "search", "--db", dbPath, "--format", "paths", "bm25")
	for _, line := range strings.Split(strings.TrimSpace(stdout), "\n") {
		if line != "" && !filepath.IsAbs(line) {
			t.Errorf("search --format paths напечатал %q, ожидался абсолютный путь", line)
		}
	}
}

// TestE2EIncompatibleSchema проверяет, что база от прежней версии senso
// не используется молча: каждая команда завершается ошибкой.
func TestE2EIncompatibleSchema(t *testing.T) {
	// Проверка ищет текст сообщения, поэтому язык вывода фиксируется.
	t.Setenv("SENSO_LANG", "en")

	root := writeTree(t, sampleTree())
	dbPath := dbIn(t)
	runIndexReport(t, dbPath, root)

	// Подделываем версию схемы так, как её видела бы прежняя версия senso.
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("UPDATE meta SET value = '3' WHERE key = 'schema_version'"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	commands := map[string][]string{
		"index":  {"index", "--quiet", "--db", dbPath, root},
		"search": {"search", "--db", dbPath, "bm25"},
		"status": {"status", "--db", dbPath},
		"check":  {"check", "--quiet", "--db", dbPath, root},
		"rm":     {"rm", "--db", dbPath, root},
	}
	for name, args := range commands {
		t.Run(name, func(t *testing.T) {
			code, _, stderr := runQuiet(t, args...)
			if code != exitError {
				t.Errorf("код завершения = %d, ожидался %d", code, exitError)
			}
			if !strings.Contains(stderr, "schema 3") {
				t.Errorf("stderr = %q, ожидалось упоминание несовместимой схемы", stderr)
			}
		})
	}
}

// TestE2EFiltersAcrossModes проверяет, что фильтры результатов работают
// одинаково в лексическом, семантическом и гибридном режимах.
func TestE2EFiltersAcrossModes(t *testing.T) {
	root := writeTree(t, map[string]string{
		"docs/guide.md":  "# Хранилище\n\nИндекс хранит чанки текста в SQLite.\n",
		"docs/legacy.md": "# Хранилище (устаревшее)\n\nПрежняя схема хранения чанков.\n",
		"src/store.go":   "package store\n\n// Хранилище чанков текста.\nfunc Store() {}\n",
	})
	dbPath := dbIn(t)
	ollama := startFakeOllama(t)

	rep := runIndexReport(t, dbPath, root, "--embed", "--model", "test-model", "--ollama", ollama)
	if !rep.Vectors {
		t.Fatalf("index --embed не построил векторы: %+v", rep)
	}

	stdout, _ := mustRun(t, "status", "--json", "--db", dbPath)
	var st statusJSONOut
	decodeJSON(t, stdout, &st)
	if st.Mode != "lexical+semantic" || st.Vectors != rep.Chunks || st.Dim != embedDim {
		t.Errorf("status = %+v, ожидались mode=lexical+semantic, vectors=%d, dim=%d", st, rep.Chunks, embedDim)
	}
	if st.Model != "test-model" {
		t.Errorf("status: model = %q, ожидалась test-model", st.Model)
	}

	modes := map[string][]string{
		"lexical":  nil,
		"semantic": {"--semantic", "--ollama", ollama},
		"hybrid":   {"--hybrid", "--ollama", ollama},
	}
	for mode, modeFlags := range modes {
		t.Run(mode, func(t *testing.T) {
			// Без фильтров находятся файлы из обоих каталогов.
			all := runSearchV2(t, dbPath, "хранилище чанков", modeFlags...)
			if all.Mode != mode {
				t.Errorf("mode = %q, ожидался %q", all.Mode, mode)
			}
			if len(all.Results) == 0 {
				t.Fatal("без фильтров нет результатов")
			}

			// --ext оставляет только markdown.
			byExt := runSearchV2(t, dbPath, "хранилище чанков", append(modeFlags, "--ext", "md")...)
			for _, name := range byExt.names() {
				if filepath.Ext(name) != ".md" {
					t.Errorf("--ext md вернул %q", name)
				}
			}
			if len(byExt.Results) == 0 {
				t.Error("--ext md не вернул ни одного результата")
			}

			// --path оставляет только совпавшее поддерево.
			byPath := runSearchV2(t, dbPath, "хранилище чанков", append(modeFlags, "--path", "**/src/**")...)
			if got := byPath.names(); len(got) != 1 || got[0] != "store.go" {
				t.Errorf("--path вернул %v, ожидался только store.go", got)
			}

			// --exclude убирает совпавшее, оставляя остальное.
			byExclude := runSearchV2(t, dbPath, "хранилище чанков", append(modeFlags, "--exclude", "**/legacy.md")...)
			if slices.Contains(byExclude.names(), "legacy.md") {
				t.Errorf("--exclude оставил legacy.md: %v", byExclude.names())
			}
			if len(byExclude.Results) == 0 {
				t.Error("--exclude убрал всю выдачу")
			}

			// --root ограничивает выдачу зарегистрированным корнем.
			byRoot := runSearchV2(t, dbPath, "хранилище чанков", append(modeFlags, "--root", root)...)
			if len(byRoot.Results) != len(all.Results) {
				t.Errorf("--root %s вернул %d результатов, ожидалось %d",
					root, len(byRoot.Results), len(all.Results))
			}
		})
	}

	// Неизвестный корень - ошибка использования, а не пустая выдача.
	if code, _, _ := runQuiet(t, "search", "--db", dbPath, "--root", filepath.Join(root, "nosuchroot"), "хранилище"); code != exitUsage {
		t.Errorf("search --root с неизвестным корнем: код = %d, ожидался %d", code, exitUsage)
	}

	// Семантический поиск по индексу без векторов не должен молча
	// подменяться лексическим.
	lexOnlyDB := dbIn(t)
	runIndexReport(t, lexOnlyDB, root)
	code, _, stderr := runQuiet(t, "search", "--db", lexOnlyDB, "--semantic", "хранилище")
	if code != exitError {
		t.Errorf("--semantic без векторов: код = %d, ожидался %d (%s)", code, exitError, stderr)
	}
}

// TestE2ECorruptDocumentAndStrict проверяет обработку повреждённого
// документа: обычный запуск сообщает об ошибке в отчёте и продолжает
// работу, --strict завершается с ошибкой.
func TestE2ECorruptDocumentAndStrict(t *testing.T) {
	root := writeTree(t, sampleTree())
	// Файл с расширением .docx, который не является zip-архивом.
	writeFileIn(t, root, "broken.docx", "это не документ, а просто текст\n")
	// Валидный zip без word/document.xml - формально архив, но не docx.
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("hello.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("not a docx")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	writeFileIn(t, root, "empty.docx", buf.String())
	dbPath := dbIn(t)

	rep := runIndexReport(t, dbPath, root)
	if len(rep.Failed) != 2 {
		t.Fatalf("failed = %+v, ожидались два повреждённых документа", rep.Failed)
	}
	for _, f := range rep.Failed {
		if f.Code != "extract_failed" {
			t.Errorf("код ошибки %q для %s, ожидался extract_failed", f.Code, f.Path)
		}
		if f.Message == "" {
			t.Errorf("пустое сообщение об ошибке для %s", f.Path)
		}
	}
	// Остальные файлы всё равно проиндексированы.
	if rep.Indexed != 3 {
		t.Errorf("indexed = %d, ожидалось 3: повреждённый файл не должен мешать остальным", rep.Indexed)
	}
	if resp := runSearchV2(t, dbPath, "bm25"); len(resp.Results) == 0 {
		t.Error("после повреждённого документа поиск по остальным файлам не работает")
	}

	// --strict превращает те же ошибки в ненулевой код завершения.
	strictDB := dbIn(t)
	code, stdout, stderr := runQuiet(t, "index", "--quiet", "--strict", "--report-json", "--db", strictDB, root)
	if code != exitError {
		t.Errorf("index --strict: код = %d, ожидался %d (%s)", code, exitError, stderr)
	}
	var strictRep indexReportJSON
	decodeJSON(t, stdout, &strictRep)
	if len(strictRep.Failed) != 2 {
		t.Errorf("index --strict: failed = %+v, ожидались два файла", strictRep.Failed)
	}
	if strictRep.Indexed != 3 {
		t.Errorf("index --strict: indexed = %d, ожидалось 3", strictRep.Indexed)
	}
}

// TestE2EHiddenFilesAndSecrets проверяет безопасное поведение по умолчанию:
// скрытые файлы и файлы с секретами не попадают в индекс, пока их не
// включили явно.
func TestE2EHiddenFilesAndSecrets(t *testing.T) {
	root := writeTree(t, map[string]string{
		"app.txt":              "обычный файл с настройками подключения\n",
		".env":                 "SECRET_TOKEN=подключение к базе\n",
		".config/settings.yml": "database: подключение\n",
		"id_rsa":               "PRIVATE KEY подключение\n",
	})
	dbPath := dbIn(t)

	// По умолчанию индексируется только обычный файл.
	rep := runIndexReport(t, dbPath, root)
	if rep.Indexed != 1 {
		t.Errorf("indexed = %d, ожидался 1 файл (%+v)", rep.Indexed, rep)
	}
	if rep.Excluded == 0 {
		t.Error("отчёт не сообщил ни об одном исключённом пути")
	}
	if got := runSearchV2(t, dbPath, "подключение").names(); !slices.Equal(got, []string{"app.txt"}) {
		t.Errorf("выдача = %v, ожидался только app.txt", got)
	}

	// --hidden включает скрытые пути, но не секреты.
	hiddenDB := dbIn(t)
	runIndexReport(t, hiddenDB, root, "--hidden")
	got := runSearchV2(t, hiddenDB, "подключение").names()
	slices.Sort(got)
	if !slices.Equal(got, []string{"app.txt", "settings.yml"}) {
		t.Errorf("с --hidden выдача = %v, ожидались app.txt и settings.yml", got)
	}

	// Секреты добавляются только точечным --include-hidden.
	secretDB := dbIn(t)
	runIndexReport(t, secretDB, root, "--include-hidden", ".env,id_rsa")
	got = runSearchV2(t, secretDB, "подключение").names()
	slices.Sort(got)
	if !slices.Equal(got, []string{".env", "app.txt", "id_rsa"}) {
		t.Errorf("с --include-hidden выдача = %v, ожидались .env, app.txt и id_rsa", got)
	}

	// --exclude сильнее любого включения.
	excludeDB := dbIn(t)
	runIndexReport(t, excludeDB, root, "--include-hidden", ".env,id_rsa", "--exclude", "**/.env")
	if got := runSearchV2(t, excludeDB, "подключение").names(); slices.Contains(got, ".env") {
		t.Errorf("--exclude не убрал .env: %v", got)
	}

	// check повторяет правила отбора, записанные при индексации, поэтому
	// видит индекс свежим и с явными флагами, и без них.
	if code, chk := runCheckJSON(t, secretDB, root, "--include-hidden", ".env,id_rsa"); code != exitOK || !chk.Fresh {
		t.Errorf("check с теми же флагами: код = %d, fresh = %v (%+v)", code, chk.Fresh, chk)
	}
	if code, chk := runCheckJSON(t, secretDB, root); code != exitOK || !chk.Fresh {
		t.Errorf("check без флагов: код = %d, fresh = %v (%+v)", code, chk.Fresh, chk)
	}

	// Явно заданные другие правила - это намерение переиндексировать
	// дерево иначе: тогда часть проиндексированного оказывается исключена.
	code, chk := runCheckJSON(t, secretDB, root, "--include-hidden", "")
	if code != exitStale || chk.Excluded != 2 {
		t.Errorf("check с другими правилами отбора: код = %d, excluded = %d, ожидались %d и 2",
			code, chk.Excluded, exitStale)
	}
}
