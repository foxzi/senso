// Command senso - консольная утилита для семантического индексирования и
// поиска по текстовым файлам в дереве каталогов.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"senso/internal/cli"
)

// helpText - текст общей справки по подкомандам.
const helpText = `senso - семантический поиск по файлам в каталоге

Использование:
  senso <команда> [аргументы]

Команды:
  index    построить или обновить индекс для указанного пути
  search   найти файлы, релевантные запросу
  status   показать статистику по текущему индексу
  rm       удалить файл или поддерево из индекса
  version  показать версию
  help     показать эту справку

Флаг --db <file> и переменная окружения SENSO_DB задают путь к базе данных.
`

func main() {
	os.Exit(run(os.Args[1:]))
}

// run выполняет выбранную подкоманду и возвращает код завершения процесса.
func run(args []string) int {
	if len(args) == 0 {
		fmt.Print(helpText)
		return 0
	}

	cmd, rest := args[0], args[1:]

	var err error
	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(helpText)
		return 0
	case "version":
		fmt.Println("senso dev")
		return 0
	case "index":
		err = cli.RunIndex(rest)
	case "search":
		err = cli.RunSearch(rest)
	case "status":
		err = cli.RunStatus(rest)
	case "rm":
		err = cli.RunRm(rest)
	default:
		fmt.Fprintf(os.Stderr, "senso: неизвестная команда %q\n", cmd)
		return 2
	}

	if err == nil {
		return 0
	}

	if errors.Is(err, flag.ErrHelp) {
		// Справка уже напечатана подкомандой в stdout - это успех.
		return 0
	}

	var usageErr *cli.UsageError
	if errors.As(err, &usageErr) {
		fmt.Fprintf(os.Stderr, "senso %s: %v\n", cmd, usageErr)
		return 2
	}

	fmt.Fprintf(os.Stderr, "senso %s: %v\n", cmd, err)
	return 1
}
