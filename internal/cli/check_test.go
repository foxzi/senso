package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"senso/internal/dbpath"
	"senso/internal/store"
	"senso/internal/walk"
)

func TestParseCheckArgsDefaults(t *testing.T) {
	opts, err := parseCheckArgs(nil)
	if err != nil {
		t.Fatalf("parseCheckArgs: %v", err)
	}
	if opts.Path != "." {
		t.Errorf("Path = %q, want \".\"", opts.Path)
	}
	if opts.Hash || opts.JSON {
		t.Errorf("Hash/JSON should be off by default: %+v", opts)
	}
	if opts.MaxFileSize != 10 {
		t.Errorf("MaxFileSize = %d, want 10", opts.MaxFileSize)
	}
}

func TestParseCheckArgsSelectionFlags(t *testing.T) {
	opts, err := parseCheckArgs([]string{"--hash", "--json", "--ext", "go,md", "--hidden", "src"})
	if err != nil {
		t.Fatalf("parseCheckArgs: %v", err)
	}
	if !opts.Hash || !opts.JSON || !opts.Hidden {
		t.Errorf("flags not applied: %+v", opts)
	}
	if opts.Ext != "go,md" {
		t.Errorf("Ext = %q", opts.Ext)
	}
	if opts.Path != "src" {
		t.Errorf("Path = %q, want \"src\"", opts.Path)
	}
}

func TestParseCheckArgsTooManyPositional(t *testing.T) {
	if _, err := parseCheckArgs([]string{"a", "b"}); err == nil {
		t.Fatal("expected usage error for two positional arguments")
	}
}

func TestParseCheckArgsZeroMaxFileSize(t *testing.T) {
	if _, err := parseCheckArgs([]string{"--max-file-size", "0"}); err == nil {
		t.Fatal("expected usage error for --max-file-size 0")
	}
}

func TestCheckReportStale(t *testing.T) {
	cases := []struct {
		name  string
		rep   checkReport
		stale bool
	}{
		{"empty", checkReport{}, false},
		{"unchanged only", checkReport{Unchanged: 5}, false},
		{"changed", checkReport{Changed: 1}, true},
		{"missing", checkReport{Missing: 1}, true},
		{"unindexed", checkReport{Unindexed: 1}, true},
		{"excluded", checkReport{Excluded: 1}, true},
		{"issue", checkReport{Issues: []checkIssue{{Code: issueNoIndex}}}, true},
		// Нечитаемый файл сам по себе не делает индекс устаревшим:
		// про него просто ничего не известно.
		{"failed only", checkReport{Failed: []reportFailure{{Path: "a"}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.rep.stale(); got != tc.stale {
				t.Errorf("stale() = %v, want %v", got, tc.stale)
			}
		})
	}
}

func TestFileChangedFastMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	state := store.FileMeta{MTime: info.ModTime().UnixNano(), Size: info.Size(), Hash: hashContent([]byte("hello"))}

	opts := checkOptions{}
	opts.MaxFileSize = 10

	changed, err := fileChanged(path, state, opts)
	if err != nil {
		t.Fatalf("fileChanged: %v", err)
	}
	if changed {
		t.Error("file with matching mtime and size must not be changed")
	}

	// Тот же текст, но другое время модификации: быстрый режим содержимое
	// не читает и обязан сообщить об изменении.
	stale := state
	stale.MTime--
	changed, err = fileChanged(path, stale, opts)
	if err != nil {
		t.Fatalf("fileChanged: %v", err)
	}
	if !changed {
		t.Error("fast mode must report mtime difference as a change")
	}
}

func TestFileChangedHashMode(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	opts := checkOptions{Hash: true}
	opts.MaxFileSize = 10

	// mtime разошёлся, содержимое то же - с --hash это не изменение.
	same := store.FileMeta{MTime: info.ModTime().UnixNano() - 1, Size: info.Size(), Hash: hashContent([]byte("hello"))}
	changed, err := fileChanged(path, same, opts)
	if err != nil {
		t.Fatalf("fileChanged: %v", err)
	}
	if changed {
		t.Error("hash mode must ignore mtime-only difference")
	}

	other := store.FileMeta{MTime: info.ModTime().UnixNano() - 1, Size: info.Size(), Hash: hashContent([]byte("other"))}
	changed, err = fileChanged(path, other, opts)
	if err != nil {
		t.Fatalf("fileChanged: %v", err)
	}
	if !changed {
		t.Error("hash mode must report content difference as a change")
	}
}

func TestFileChangedMissingFile(t *testing.T) {
	opts := checkOptions{}
	opts.MaxFileSize = 10
	changed, err := fileChanged(filepath.Join(t.TempDir(), "nope.txt"), store.FileMeta{Hash: "x"}, opts)
	if err != nil {
		t.Fatalf("fileChanged: %v", err)
	}
	if !changed {
		t.Error("vanished file must be reported as changed")
	}
}

func TestPrintCheckSummaryFresh(t *testing.T) {
	var buf bytes.Buffer
	rep := newCheckReport()
	rep.Fresh = true
	rep.Scanned = 7
	rep.IndexedAt = "2026-01-01T00:00:00Z"
	rep.Database = "/tmp/x/.senso/index.db"
	printCheckSummary(&buf, rep, "/tmp/x")

	out := buf.String()
	for _, want := range []string{"7", "2026-01-01T00:00:00Z", ".senso/index.db"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary %q must contain %q", out, want)
		}
	}
}

func TestPrintCheckSummaryStale(t *testing.T) {
	var buf bytes.Buffer
	rep := newCheckReport()
	rep.Changed = 2
	rep.Missing = 1
	rep.addIssue(issueModelMismatch, "model differs")
	rep.addFailure("/tmp/x/bad.txt", failRead, errors.New("permission denied"))
	printCheckSummary(&buf, rep, "/tmp/x")

	out := buf.String()
	for _, want := range []string{issueModelMismatch, "model differs", "bad.txt", failRead} {
		if !strings.Contains(out, want) {
			t.Errorf("summary %q must contain %q", out, want)
		}
	}
}

// runCheckIn выполняет RunCheck из каталога dir и возвращает код завершения.
func runCheckIn(t *testing.T, dir string, args ...string) int {
	t.Helper()

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)

	err = RunCheck(args)
	if err == nil {
		return 0
	}
	var exitErr *ExitError
	if errors.As(err, &exitErr) {
		return exitErr.Code
	}
	t.Fatalf("RunCheck: %v", err)
	return -1
}

func TestRunCheckWithoutDatabase(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runCheckIn(t, dir, "--quiet"); code != exitStale {
		t.Errorf("exit code = %d, want %d", code, exitStale)
	}
}

func TestRunCheckLifecycle(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	if err := os.WriteFile(path, []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)

	if err := RunIndex([]string{"--quiet", "."}); err != nil {
		t.Fatalf("RunIndex: %v", err)
	}

	if code := runCheckIn(t, dir, "--quiet"); code != 0 {
		t.Errorf("fresh index: exit = %d, want 0", code)
	}

	// Новый файл делает индекс устаревшим.
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("second"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code := runCheckIn(t, dir, "--quiet"); code != exitStale {
		t.Errorf("new file: exit = %d, want %d", code, exitStale)
	}

	if err := RunIndex([]string{"--quiet", "."}); err != nil {
		t.Fatalf("RunIndex: %v", err)
	}
	if code := runCheckIn(t, dir, "--quiet"); code != 0 {
		t.Errorf("after reindex: exit = %d, want 0", code)
	}

	// Удалённый файл тоже расхождение.
	if err := os.Remove(filepath.Join(dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
	if code := runCheckIn(t, dir, "--quiet"); code != exitStale {
		t.Errorf("removed file: exit = %d, want %d", code, exitStale)
	}
}

// TestExcludeReasonUsesAncestorDir проверяет, что для файла внутри
// исключённого каталога причина берётся у каталога-предка.
func TestExcludeReasonUsesAncestorDir(t *testing.T) {
	reasons := map[string]string{
		"/p/build":     walk.ReasonGitignore,
		"/p/a/one.key": walk.ReasonSecret,
	}

	cases := map[string]string{
		"/p/build/out/app.js": walk.ReasonGitignore,
		"/p/a/one.key":        walk.ReasonSecret,
		"/p/a/two.go":         reasonUnknown,
	}
	for path, want := range cases {
		if got := excludeReason(path, reasons); got != want {
			t.Errorf("excludeReason(%s) = %q, ожидалось %q", path, got, want)
		}
	}
}

// TestRunCheckReportsExcludeReason проверяет, что файл, выпавший из выборки
// из-за нового правила, попадает в отчёт с кодом причины.
func TestRunCheckReportsExcludeReason(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("world"), 0o644); err != nil {
		t.Fatal(err)
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)

	if err := RunIndex([]string{"--quiet", "."}); err != nil {
		t.Fatalf("RunIndex: %v", err)
	}

	opts, err := parseCheckArgs([]string{"--exclude", "b.txt", "."})
	if err != nil {
		t.Fatalf("parseCheckArgs: %v", err)
	}
	dbPath, err := dbpath.Find(opts.DB)
	if err != nil {
		t.Fatalf("dbpath.Find: %v", err)
	}
	s, err := store.OpenReadOnly(dbPath)
	if err != nil {
		t.Fatalf("OpenReadOnly: %v", err)
	}
	defer s.Close()

	root, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	rep := newCheckReport()
	if err := compareTree(root, opts, s, rep); err != nil {
		t.Fatalf("compareTree: %v", err)
	}

	if rep.Excluded != 1 || rep.ExcludedByReason[walk.ReasonExcludeGlob] != 1 {
		t.Fatalf("Excluded = %d, ExcludedByReason = %v", rep.Excluded, rep.ExcludedByReason)
	}
}
