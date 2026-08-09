package dbpath

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFindUpFindsParent(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(root, "a", "b", "c")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, ok := findUp(sub)
	if !ok {
		t.Fatal("findUp не нашёл .senso в родительской директории")
	}
	if got != root {
		t.Errorf("findUp = %q, ожидалось %q", got, root)
	}
}

func TestFindUpNotFound(t *testing.T) {
	// Изолированная временная директория без .senso где-либо выше -
	// t.TempDir() обычно лежит в /tmp, где .senso не создаётся тестами.
	dir := t.TempDir()
	if _, ok := findUp(dir); ok {
		// Если случайно нашлась чужая .senso выше по дереву /tmp - тест не показателен,
		// но такое практически невозможно в CI/локальном окружении.
		t.Skip("в родительских директориях неожиданно найдена .senso, тест недостоверен")
	}
}

func TestFindFlagOverridesEnv(t *testing.T) {
	t.Setenv(EnvVar, "/env/path/index.db")

	got, err := Find("flag/path.db")
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Abs("flag/path.db")
	if got != want {
		t.Errorf("Find = %q, ожидалось %q (флаг должен иметь приоритет)", got, want)
	}
}

func TestFindUsesEnvVar(t *testing.T) {
	dir := t.TempDir()
	envPath := filepath.Join(dir, "custom.db")
	t.Setenv(EnvVar, envPath)

	got, err := Find("")
	if err != nil {
		t.Fatal(err)
	}
	if got != envPath {
		t.Errorf("Find = %q, ожидалось %q", got, envPath)
	}
}

func TestFindNotFound(t *testing.T) {
	t.Setenv(EnvVar, "")
	dir := t.TempDir()

	if _, ok := findUp(dir); ok {
		t.Skip("в родительских директориях неожиданно найдена .senso, тест недостоверен")
	}

	oldWd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(oldWd); err != nil {
			t.Fatal(err)
		}
	}()
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}

	if _, err := Find(""); !errors.Is(err, ErrNotFound) {
		t.Errorf("Find вернул %v, ожидалась ErrNotFound", err)
	}
}

func TestCreateCreatesDirAndGitignore(t *testing.T) {
	t.Setenv(EnvVar, "")
	root := t.TempDir()
	// Убедимся, что в root и выше нет .senso, иначе Find найдёт её раньше Create.
	if _, ok := findUp(root); ok {
		t.Skip("в родительских директориях неожиданно найдена .senso, тест недостоверен")
	}

	path, err := Create("", root)
	if err != nil {
		t.Fatal(err)
	}

	want := filepath.Join(root, DirName, FileName)
	if path != want {
		t.Errorf("Create = %q, ожидалось %q", path, want)
	}

	senseDir := filepath.Join(root, DirName)
	if info, err := os.Stat(senseDir); err != nil || !info.IsDir() {
		t.Fatalf("директория %s не создана", senseDir)
	}

	gitignore := filepath.Join(senseDir, ".gitignore")
	data, err := os.ReadFile(gitignore)
	if err != nil {
		t.Fatalf("не удалось прочитать .gitignore: %v", err)
	}
	if string(data) != "*\n" {
		t.Errorf(".gitignore содержит %q, ожидалось %q", data, "*\n")
	}
}

func TestCreateDoesNotOverwriteGitignore(t *testing.T) {
	root := t.TempDir()
	senseDir := filepath.Join(root, DirName)
	if err := os.MkdirAll(senseDir, 0o755); err != nil {
		t.Fatal(err)
	}
	custom := "custom content\n"
	if err := os.WriteFile(filepath.Join(senseDir, ".gitignore"), []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := writeGitignore(senseDir); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(senseDir, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != custom {
		t.Errorf(".gitignore перезаписан: %q", data)
	}
}
