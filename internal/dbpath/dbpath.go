// Package dbpath отвечает за поиск и создание файла базы данных senso.
//
// Приоритет определения пути к базе:
//  1. явный флаг --db;
//  2. переменная окружения SENSO_DB;
//  3. директория .senso, найденная вверх по дереву от текущей директории
//     (аналогично тому, как git ищет .git).
package dbpath

import (
	"errors"
	"os"
	"path/filepath"
)

// DirName - имя служебной директории с базой данных.
const DirName = ".senso"

// FileName - имя файла базы данных внутри DirName.
const FileName = "index.db"

// EnvVar - имя переменной окружения с явным путём к базе.
const EnvVar = "SENSO_DB"

// ErrNotFound возвращается, когда директория .senso не найдена
// ни по флагу, ни по переменной окружения, ни поиском вверх по дереву.
var ErrNotFound = errors.New("dbpath: .senso directory not found")

// Find определяет путь к файлу базы данных по приоритету:
// флаг --db, переменная окружения SENSO_DB, поиск .senso вверх по дереву.
func Find(flagDB string) (string, error) {
	if flagDB != "" {
		return filepath.Abs(flagDB)
	}
	if envDB := os.Getenv(EnvVar); envDB != "" {
		return filepath.Abs(envDB)
	}
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	dir, ok := findUp(cwd)
	if !ok {
		return "", ErrNotFound
	}
	return filepath.Join(dir, DirName, FileName), nil
}

// findUp ищет директорию DirName, начиная с startDir и поднимаясь к
// родительским директориям до корня файловой системы. Возвращает
// директорию, в которой найдена .senso (не саму .senso).
func findUp(startDir string) (string, bool) {
	dir := startDir
	for {
		candidate := filepath.Join(dir, DirName)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// Create определяет путь к базе так же, как Find, но если база не найдена,
// создаёт директорию <root>/.senso и возвращает путь к новому файлу базы.
func Create(flagDB, root string) (string, error) {
	path, err := Find(flagDB)
	if err == nil {
		return path, nil
	}
	if !errors.Is(err, ErrNotFound) {
		return "", err
	}

	absRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	senseDir := filepath.Join(absRoot, DirName)
	if err := os.MkdirAll(senseDir, 0o755); err != nil {
		return "", err
	}
	if err := writeGitignore(senseDir); err != nil {
		return "", err
	}
	return filepath.Join(senseDir, FileName), nil
}

// writeGitignore создаёт файл .gitignore внутри directory, исключающий
// все файлы из git. Если файл уже существует, не перезаписывает его.
func writeGitignore(directory string) error {
	path := filepath.Join(directory, ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	return os.WriteFile(path, []byte("*\n"), 0o644)
}
