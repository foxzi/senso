package walk

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// mustWrite создаёт файл с заданным содержимым, включая промежуточные
// директории.
func mustWrite(t *testing.T, path string, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

// collect запускает Walk и возвращает отсортированный список
// относительных путей найденных файлов.
func collect(t *testing.T, root string, opts Options) []string {
	t.Helper()
	var got []string
	err := Walk(root, opts, func(f File) error {
		got = append(got, f.Rel)
		return nil
	}, func(path string, err error) {
		t.Errorf("неожиданная ошибка обхода %s: %v", path, err)
	})
	if err != nil {
		t.Fatalf("Walk: %v", err)
	}
	sort.Strings(got)
	return got
}

func TestWalkFindsFilesWithoutExtension(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "Makefile"), "all:\n\techo hi\n")
	mustWrite(t, filepath.Join(root, "README"), "readme content")

	got := collect(t, root, Options{})

	want := []string{"Makefile", "README"}
	assertEqual(t, got, want)
}

func TestWalkExtFilterIsCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.md"), "markdown content")
	mustWrite(t, filepath.Join(root, "b.txt"), "text content")

	got := collect(t, root, Options{Ext: []string{".MD"}})

	assertEqual(t, got, []string{"a.md"})
}

func TestWalkExtFilterAcceptsWithAndWithoutDot(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "a.go"), "package a")
	mustWrite(t, filepath.Join(root, "b.py"), "print(1)")

	got := collect(t, root, Options{Ext: []string{"go"}})

	assertEqual(t, got, []string{"a.go"})
}

func TestWalkSkipsHiddenGitNodeModulesVendorDirs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".hidden", "secret.txt"), "secret content")
	mustWrite(t, filepath.Join(root, ".git", "config"), "git config content")
	mustWrite(t, filepath.Join(root, "node_modules", "pkg", "index.js"), "js content")
	mustWrite(t, filepath.Join(root, "vendor", "lib", "lib.go"), "package lib")
	mustWrite(t, filepath.Join(root, "keep.txt"), "kept content")

	got := collect(t, root, Options{})

	assertEqual(t, got, []string{"keep.txt"})
}

func TestWalkSkipsNoisyFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "package-lock.json"), "{}")
	mustWrite(t, filepath.Join(root, "app.min.js"), "var a=1;")
	mustWrite(t, filepath.Join(root, "icon.svg"), "<svg></svg>")
	mustWrite(t, filepath.Join(root, "yarn.lock"), "lock content")
	mustWrite(t, filepath.Join(root, "main.go"), "package main")

	got := collect(t, root, Options{})

	assertEqual(t, got, []string{"main.go"})
}

func TestWalkExcludeGlobDoubleStar(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "docs", "guide.txt"), "guide content")
	mustWrite(t, filepath.Join(root, "docs", "sub", "deep.txt"), "deep content")
	mustWrite(t, filepath.Join(root, "notes.tmp"), "tmp content")
	mustWrite(t, filepath.Join(root, "sub", "notes.tmp"), "tmp content 2")
	mustWrite(t, filepath.Join(root, "keep.txt"), "kept content")

	got := collect(t, root, Options{Exclude: []string{"docs/**", "**/*.tmp"}})

	assertEqual(t, got, []string{"keep.txt"})
}

func TestWalkMaxFileSizeAndEmptyFiles(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "big.txt"), "0123456789")
	mustWrite(t, filepath.Join(root, "small.txt"), "0123")
	mustWrite(t, filepath.Join(root, "empty.txt"), "")

	got := collect(t, root, Options{MaxFileSize: 5})

	assertEqual(t, got, []string{"small.txt"})
}

func TestWalkGitignoreRoot(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".gitignore"), "ignored.txt\n")
	mustWrite(t, filepath.Join(root, "ignored.txt"), "should be ignored")
	mustWrite(t, filepath.Join(root, "keep.txt"), "kept content")

	// Сам .gitignore скрытый, поэтому в индекс не попадает.
	got := collect(t, root, Options{UseGitignore: true})

	assertEqual(t, got, []string{"keep.txt"})
}

func TestWalkGitignoreNestedAppliesOnlyToSubtree(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "sub", ".gitignore"), "local.txt\n")
	mustWrite(t, filepath.Join(root, "sub", "local.txt"), "ignored in sub")
	mustWrite(t, filepath.Join(root, "local.txt"), "not ignored at root")
	mustWrite(t, filepath.Join(root, "sub", "keep.txt"), "kept in sub")

	got := collect(t, root, Options{UseGitignore: true})

	assertEqual(t, got, []string{"local.txt", "sub/keep.txt"})
}

func TestWalkGitignoreDisabled(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".gitignore"), "ignored.txt\n")
	mustWrite(t, filepath.Join(root, "ignored.txt"), "should not be ignored")

	got := collect(t, root, Options{UseGitignore: false})

	assertEqual(t, got, []string{"ignored.txt"})
}

func TestWalkSymlinkToParentDoesNotLoop(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	mustWrite(t, filepath.Join(sub, "file.txt"), "file content")

	link := filepath.Join(sub, "loop")
	if err := os.Symlink(root, link); err != nil {
		t.Skipf("симлинки не поддерживаются: %v", err)
	}

	got := collect(t, root, Options{})

	assertEqual(t, got, []string{"sub/file.txt"})
}

// assertEqual сравнивает отсортированные срезы относительных путей.
func assertEqual(t *testing.T, got, want []string) {
	t.Helper()
	sort.Strings(want)
	if len(got) != len(want) {
		t.Fatalf("получено %v, ожидалось %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("получено %v, ожидалось %v", got, want)
		}
	}
}

func TestWalkSkipsHiddenFilesByDefault(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".env"), "SECRET_TOKEN=abcdef")
	mustWrite(t, filepath.Join(root, ".editorconfig"), "root = true")
	mustWrite(t, filepath.Join(root, "main.go"), "package main")

	got := collect(t, root, Options{})

	assertEqual(t, got, []string{"main.go"})
}

func TestWalkHiddenIncludesHiddenPathsButNotSecrets(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "name: ci")
	mustWrite(t, filepath.Join(root, ".editorconfig"), "root = true")
	mustWrite(t, filepath.Join(root, ".env"), "SECRET_TOKEN=abcdef")
	mustWrite(t, filepath.Join(root, ".env.local"), "SECRET_TOKEN=local")
	mustWrite(t, filepath.Join(root, "deploy", "server.pem"), "PRIVATE KEY")
	mustWrite(t, filepath.Join(root, "main.go"), "package main")

	got := collect(t, root, Options{Hidden: true})

	assertEqual(t, got, []string{".editorconfig", ".github/workflows/ci.yml", "main.go"})
}

func TestWalkHiddenKeepsHardExcludedDirs(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".git", "config"), "git config content")
	mustWrite(t, filepath.Join(root, ".senso", "index.db"), "db content")
	mustWrite(t, filepath.Join(root, ".github", "ci.yml"), "name: ci")

	got := collect(t, root, Options{Hidden: true})

	assertEqual(t, got, []string{".github/ci.yml"})
}

func TestWalkIncludeHiddenOpensSubtreeOnly(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".github", "workflows", "ci.yml"), "name: ci")
	mustWrite(t, filepath.Join(root, ".agents", "notes.md"), "agent notes")
	mustWrite(t, filepath.Join(root, ".editorconfig"), "root = true")
	mustWrite(t, filepath.Join(root, "main.go"), "package main")

	got := collect(t, root, Options{IncludeHidden: []string{".github/**"}})

	assertEqual(t, got, []string{".github/workflows/ci.yml", "main.go"})
}

func TestWalkIncludeHiddenOpensSecretExplicitly(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, ".env"), "SECRET_TOKEN=abcdef")
	mustWrite(t, filepath.Join(root, ".env.local"), "SECRET_TOKEN=local")
	mustWrite(t, filepath.Join(root, "main.go"), "package main")

	got := collect(t, root, Options{IncludeHidden: []string{".env"}})

	assertEqual(t, got, []string{".env", "main.go"})
}

func TestWalkSecretsExcludedWithoutGitignore(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, filepath.Join(root, "config", "database.key"), "private key")
	mustWrite(t, filepath.Join(root, "config", "id_rsa"), "private key")
	mustWrite(t, filepath.Join(root, "config", "app.yaml"), "app: 1")

	got := collect(t, root, Options{})

	assertEqual(t, got, []string{"config/app.yaml"})
}
