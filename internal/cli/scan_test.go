package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestDecideFile(t *testing.T) {
	cases := []struct {
		name              string
		dbMtime, dbSize   int64
		dbHash            string
		curMtime, curSize int64
		curHash           string
		want              fileAction
	}{
		{
			name:   "нет в базе",
			dbHash: "",
			want:   actionReindex,
		},
		{
			name:    "mtime и size совпали",
			dbMtime: 100, dbSize: 10, dbHash: "aaa",
			curMtime: 100, curSize: 10, curHash: "bbb", // хэш не проверяется на этом пути
			want: actionSkip,
		},
		{
			name:    "метаданные изменились, хэш совпал",
			dbMtime: 100, dbSize: 10, dbHash: "aaa",
			curMtime: 200, curSize: 10, curHash: "aaa",
			want: actionTouch,
		},
		{
			name:    "содержимое изменилось",
			dbMtime: 100, dbSize: 10, dbHash: "aaa",
			curMtime: 200, curSize: 20, curHash: "ccc",
			want: actionReindex,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := decideFile(c.dbMtime, c.dbSize, c.dbHash, c.curMtime, c.curSize, c.curHash)
			if got != c.want {
				t.Errorf("decideFile() = %v, ожидалось %v", got, c.want)
			}
		})
	}
}

func TestPrefixInSubtree(t *testing.T) {
	cases := []struct {
		name             string
		absPath, absRoot string
		want             bool
	}{
		{"равен корню", "/a/b", "/a/b", true},
		{"вложен на один уровень", "/a/b/c", "/a/b", true},
		{"вложен на несколько уровней", "/a/b/c/d", "/a/b", true},
		{"похожее имя без разделителя", "/a/bc", "/a/b", false},
		{"посторонний путь", "/a/c", "/a/b", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := prefixInSubtree(c.absPath, c.absRoot)
			if got != c.want {
				t.Errorf("prefixInSubtree(%q, %q) = %v, ожидалось %v", c.absPath, c.absRoot, got, c.want)
			}
		})
	}
}

func TestHashContent(t *testing.T) {
	h1 := hashContent([]byte("hello"))
	h2 := hashContent([]byte("hello"))
	h3 := hashContent([]byte("world"))

	if h1 != h2 {
		t.Errorf("хэш недетерминирован: %q != %q", h1, h2)
	}
	if h1 == h3 {
		t.Errorf("хэши разных данных совпали: %q", h1)
	}
	if h1 == "" {
		t.Error("хэш пустой строкой быть не должен")
	}
}

// writeTestFile создаёт файл rel внутри dir вместе с промежуточными
// директориями и записывает в него содержимое content.
func writeTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("MkdirAll(%s): %v", filepath.Dir(full), err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("WriteFile(%s): %v", full, err)
	}
}

func TestScanFiles(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "a.md", "# a")
	writeTestFile(t, dir, "b.txt", "b")
	writeTestFile(t, dir, "node_modules/x.md", "x")
	writeTestFile(t, dir, ".git/cfg", "cfg")
	writeTestFile(t, dir, "sub/c.md", "c")
	writeTestFile(t, dir, "package-lock.json", "{}")
	writeTestFile(t, dir, "app.min.js", "var x=1;")

	opts := indexOptions{MaxFileSize: 10}

	got, err := scanFiles(dir, opts, nil)
	if err != nil {
		t.Fatalf("scanFiles: %v", err)
	}

	want := []string{
		filepath.Join(dir, "a.md"),
		filepath.Join(dir, "b.txt"),
		filepath.Join(dir, "sub", "c.md"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanFiles() = %v, ожидалось %v", got, want)
	}
}

func TestScanFilesExtFilter(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "a.md", "# a")
	writeTestFile(t, dir, "b.txt", "b")

	opts := indexOptions{MaxFileSize: 10, Ext: "md"}

	got, err := scanFiles(dir, opts, nil)
	if err != nil {
		t.Fatalf("scanFiles: %v", err)
	}

	want := []string{filepath.Join(dir, "a.md")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanFiles() с --ext md = %v, ожидалось %v", got, want)
	}
}

func TestScanFilesExcludeGlob(t *testing.T) {
	dir := t.TempDir()

	writeTestFile(t, dir, "a.md", "# a")
	writeTestFile(t, dir, "sub/c.md", "c")

	opts := indexOptions{MaxFileSize: 10, Exclude: "sub/**"}

	got, err := scanFiles(dir, opts, nil)
	if err != nil {
		t.Fatalf("scanFiles: %v", err)
	}

	want := []string{filepath.Join(dir, "a.md")}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("scanFiles() с --exclude sub/** = %v, ожидалось %v", got, want)
	}
}
