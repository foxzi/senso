// Package cli содержит реализации подкоманд утилиты senso.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"senso/internal/dbpath"
	"senso/internal/store"
)

// errHelp - сентинельная ошибка запроса справки (-h/--help). Подкоманды
// возвращают её наверх как есть, без оборачивания в UsageError, чтобы
// main.go отличал запрос справки (код выхода 0) от ошибки разбора
// аргументов (код 2).
var errHelp = flag.ErrHelp

// finishParse обрабатывает результат fs.Parse для подкоманд, которые перед
// вызовом Parse гасят собственный вывод FlagSet через fs.SetOutput(io.Discard).
// При запросе справки печатает её в stdout и возвращает errHelp как есть;
// прочие ошибки разбора оборачивает в UsageError (код выхода 2).
func finishParse(fs *flag.FlagSet, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errHelp) {
		fs.SetOutput(os.Stdout)
		fs.Usage()
		return errHelp
	}
	return &UsageError{Err: err}
}

// UsageError сигнализирует об ошибке в аргументах командной строки.
// main.go преобразует такую ошибку в код выхода 2.
type UsageError struct {
	Err error
}

func (e *UsageError) Error() string {
	return e.Err.Error()
}

// usagef создаёт UsageError с форматированным сообщением.
func usagef(format string, a ...any) error {
	return &UsageError{Err: fmt.Errorf(format, a...)}
}

// shortenPath сокращает abs относительно cwd: если abs лежит внутри cwd,
// возвращает относительный путь; если abs == cwd, возвращает ".";
// иначе возвращает abs без изменений.
func shortenPath(abs, cwd string) string {
	if abs == cwd {
		return "."
	}
	rel, err := filepath.Rel(cwd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return rel
}

// snippet схлопывает переводы строк и повторяющиеся пробелы в s в один
// пробел и обрезает результат по рунам до maxRunes символов, добавляя
// многоточие при обрезке.
func snippet(s string, maxRunes int) string {
	collapsed := strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(collapsed) <= maxRunes {
		return collapsed
	}
	runes := []rune(collapsed)
	return string(runes[:maxRunes]) + "..."
}

// openStore находит и открывает базу данных senso по флагу --db.
// Возвращает открытый стор и путь к файлу базы.
func openStore(flagDB string) (*store.Store, string, error) {
	path, err := dbpath.Find(flagDB)
	if err != nil {
		if errors.Is(err, dbpath.ErrNotFound) {
			return nil, "", errors.New("индекс не найден: запустите senso index <путь>")
		}
		return nil, "", err
	}
	s, err := store.Open(path)
	if err != nil {
		return nil, "", err
	}
	if err := s.CheckSchema(); err != nil {
		s.Close()
		return nil, "", err
	}
	return s, path, nil
}
