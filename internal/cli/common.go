// Package cli содержит реализации подкоманд утилиты senso.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"senso/internal/dbpath"
	"senso/internal/i18n"
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

// errIndexNotFound - ошибка «индекс не найден», единая для случая, когда
// .senso не найдена поиском вверх по дереву, и для случая, когда указанный
// явно файл базы не существует. Несёт машиночитаемый код errCodeNoIndex
// (см. errcode.go).
func errIndexNotFound() error {
	return withCode(errCodeNoIndex, errors.New(i18n.T("index not found: run senso index <path>", "индекс не найден: запустите senso index <путь>")))
}

// openStore находит и открывает базу данных senso по флагу --db.
// Возвращает открытый стор и путь к файлу базы.
//
// Существование файла проверяется до store.Open: драйвер SQLite создаёт файл
// базы при первом же обращении, поэтому без этой проверки читающая команда с
// опечаткой в --db молча создавала бы пустую базу и падала бы потом на
// отсутствующей таблице вместо понятного «индекс не найден».
func openStore(ctx context.Context, flagDB string) (*store.Store, string, error) {
	path, err := dbpath.Find(flagDB)
	if err != nil {
		if errors.Is(err, dbpath.ErrNotFound) {
			return nil, "", errIndexNotFound()
		}
		return nil, "", err
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, "", errIndexNotFound()
		}
		return nil, "", err
	}
	s, err := store.Open(ctx, path)
	if err != nil {
		return nil, "", err
	}
	if err := s.CheckSchema(ctx); err != nil {
		s.Close()
		return nil, "", err
	}
	return s, path, nil
}

// ExitError задаёт код завершения процесса явно - для случаев, когда
// ненулевой код не означает внутреннюю ошибку: прерывание сигналом (130)
// или обнаруженные ошибки файлов в строгом режиме.
type ExitError struct {
	Code int
	Err  error
}

func (e *ExitError) Error() string { return e.Err.Error() }

func (e *ExitError) Unwrap() error { return e.Err }
