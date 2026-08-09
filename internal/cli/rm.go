package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"senso/internal/i18n"
)

// rmOptions хранит разобранные флаги команды rm.
type rmOptions struct {
	DB string
}

// rmFlagSet создаёт FlagSet подкоманды rm, объявляет в opts все её флаги
// и возвращает FlagSet без вызова Parse. Используется как при разборе
// аргументов, так и при построении текста справки - это единственное место,
// где перечислены флаги rm.
func rmFlagSet(opts *rmOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("rm", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DB, "db", "", i18n.T("path to the database file", "путь к файлу базы данных"))
	return fs
}

// RunRm реализует подкоманду "rm": удаляет из индекса файл или всё
// поддерево по указанному пути. Файлы на диске не затрагиваются.
func RunRm(args []string) error {
	var opts rmOptions
	fs := rmFlagSet(&opts)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), commandHelpText(rmUsageLine(), rmSummary(), fs))
	}
	if err := fs.Parse(args); err != nil {
		return finishParse(fs, err)
	}
	if fs.NArg() != 1 {
		return usagef("%s", i18n.T("rm: exactly one argument is required - the path", "rm: требуется ровно один аргумент - путь"))
	}

	target, err := filepath.Abs(fs.Arg(0))
	if err != nil {
		return err
	}

	s, _, err := openStore(opts.DB)
	if err != nil {
		return err
	}
	defer s.Close()

	n, err := s.DeleteSubtree(target)
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err == nil {
		target = shortenPath(target, cwd)
	}
	fmt.Printf(i18n.T("removed files from index: %d (%s)\n", "удалено файлов из индекса: %d (%s)\n"), n, target)
	return nil
}
