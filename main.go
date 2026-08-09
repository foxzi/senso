// Command senso - консольная утилита для семантического индексирования и
// поиска по текстовым файлам в дереве каталогов.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"

	"senso/internal/cli"
	"senso/internal/i18n"
	"senso/internal/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

// run выполняет выбранную подкоманду и возвращает код завершения процесса.
func run(args []string) int {
	i18n.Set(i18n.Detect(os.Getenv))

	if len(args) == 0 {
		fmt.Print(cli.HelpText())
		return 0
	}

	cmd, rest := args[0], args[1:]

	var err error
	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(cli.HelpText())
		return 0
	case "version", "--version":
		fmt.Println(version.String())
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
		fmt.Fprintf(os.Stderr, i18n.T("senso: unknown command %q\n", "senso: неизвестная команда %q\n"), cmd)
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
