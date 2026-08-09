package cli

import (
	"flag"
	"fmt"
	"strings"
	"unicode/utf8"

	"senso/internal/i18n"
)

// helpWidth - примерная ширина в символах, по которой переносятся длинные
// описания флагов в справке. Терминал не определяется, ширина фиксированная.
const helpWidth = 80

// commandSpec описывает одну команду senso для отрисовки в справке: строку
// использования, краткое описание и (если есть) FlagSet с её флагами.
// fs берётся из тех же функций регистрации флагов, что и разбор аргументов
// команды, поэтому описания флагов никогда не дублируются вручную.
type commandSpec struct {
	usage   string
	summary string
	fs      *flag.FlagSet // nil у команд без флагов (version, help)
}

// indexUsageLine и indexSummary описывают команду index для справки.
func indexUsageLine() string { return "index [flags] <path>" }
func indexSummary() string {
	return i18n.T(
		"build or update the index for the given path",
		"построить или обновить индекс для указанного пути",
	)
}

// searchUsageLine и searchSummary описывают команду search для справки.
func searchUsageLine() string { return "search [flags] <query>" }
func searchSummary() string {
	return i18n.T(
		"find files relevant to a query",
		"найти файлы, релевантные запросу",
	)
}

// statusUsageLine и statusSummary описывают команду status для справки.
func statusUsageLine() string { return "status [flags]" }
func statusSummary() string {
	return i18n.T(
		"show statistics for the current index",
		"показать статистику по текущему индексу",
	)
}

// rmUsageLine и rmSummary описывают команду rm для справки.
func rmUsageLine() string { return "rm [flags] <path>" }
func rmSummary() string {
	return i18n.T(
		"remove a file or subtree from the index",
		"удалить файл или поддерево из индекса",
	)
}

// versionUsageLine и versionSummary описывают команду version для справки.
// У этой команды нет флагов.
func versionUsageLine() string { return "version" }
func versionSummary() string {
	return i18n.T("show the version", "показать версию")
}

// helpUsageLine и helpSummary описывают команду help для справки.
// У этой команды нет флагов.
func helpUsageLine() string { return "help" }
func helpSummary() string {
	return i18n.T("show this help", "показать эту справку")
}

// commandSpecs возвращает описания всех команд senso в порядке вывода
// в общей справке. FlagSet для каждой команды строится той же функцией
// регистрации флагов, что и при разборе аргументов подкоманды.
func commandSpecs() []commandSpec {
	return []commandSpec{
		{indexUsageLine(), indexSummary(), indexFlagSet(&indexOptions{})},
		{searchUsageLine(), searchSummary(), searchFlagSet(&searchOptions{})},
		{statusUsageLine(), statusSummary(), statusFlagSet(&statusOptions{})},
		{rmUsageLine(), rmSummary(), rmFlagSet(&rmOptions{})},
		{versionUsageLine(), versionSummary(), nil},
		{helpUsageLine(), helpSummary(), nil},
	}
}

// HelpText возвращает полный текст общей справки senso: заголовок,
// строку использования и блок команд, для каждой из которых показаны
// её флаги. Флаги берутся из тех же FlagSet, что используются при разборе
// аргументов, поэтому текст справки не может разойтись с реальными флагами.
func HelpText() string {
	var b strings.Builder

	b.WriteString(i18n.T(
		"senso - semantic search over files in a directory",
		"senso - семантический поиск по файлам в каталоге",
	))
	b.WriteString("\n\n")

	b.WriteString(i18n.T("Usage:", "Использование:"))
	b.WriteString(i18n.T(
		"\n  senso <command> [flags] [arguments]\n\n",
		"\n  senso <команда> [флаги] [аргументы]\n\n",
	))

	b.WriteString(i18n.T("Commands:", "Команды:"))
	b.WriteString("\n")
	for _, c := range commandSpecs() {
		b.WriteString(formatCommand(c.usage, c.summary, c.fs))
	}

	b.WriteString(i18n.T(
		"Flags must come before positional arguments: senso search --paths-only \"query\".\n",
		"Флаги указываются до позиционных аргументов: senso search --paths-only \"запрос\".\n",
	))
	b.WriteString(i18n.T(
		"SENSO_DB sets the path to the database, SENSO_LANG sets the output language.\n",
		"SENSO_DB задаёт путь к базе данных, SENSO_LANG задаёт язык вывода.\n",
	))

	return b.String()
}

// commandHelpText возвращает текст справки для одной команды в том же
// оформлении, что и в общей справке. Используется в качестве fs.Usage
// для команд, у которых есть свой FlagSet.
func commandHelpText(usage, summary string, fs *flag.FlagSet) string {
	var b strings.Builder
	b.WriteString(i18n.T("Usage:", "Использование:"))
	b.WriteString("\n")
	b.WriteString(formatCommand(usage, summary, fs))
	return b.String()
}

// formatCommand отрисовывает один блок команды: строку использования
// с кратким описанием и, если у команды есть флаги, список флагов под ней
// с отступом.
func formatCommand(usage, summary string, fs *flag.FlagSet) string {
	var b strings.Builder
	b.WriteString("  ")
	b.WriteString(usage)
	b.WriteString("  ")
	b.WriteString(summary)
	b.WriteString("\n")
	if fs != nil {
		b.WriteString(renderFlags(fs, "      "))
	}
	b.WriteString("\n")
	return b.String()
}

// flagEntry - одна строка колонки флагов: левая часть ("--name type")
// и правая часть (описание с добавленным значением по умолчанию).
type flagEntry struct {
	head string
	desc string
}

// renderFlags обходит все флаги fs (через VisitAll, без fs.PrintDefaults)
// и отрисовывает их в две колонки с отступом indent. Описания длиннее
// одной строки переносятся с выравниванием по второй колонке.
func renderFlags(fs *flag.FlagSet, indent string) string {
	var entries []flagEntry
	fs.VisitAll(func(f *flag.Flag) {
		name, usage := flag.UnquoteUsage(f)
		head := "--" + f.Name
		if name != "" {
			head += " " + name
		}
		if suffix := defaultSuffix(f, name); suffix != "" {
			usage += " " + suffix
		}
		entries = append(entries, flagEntry{head: head, desc: usage})
	})
	if len(entries) == 0 {
		return ""
	}

	width := 0
	for _, e := range entries {
		if len(e.head) > width {
			width = len(e.head)
		}
	}

	descWidth := helpWidth - len(indent) - width - 2
	if descWidth < 20 {
		descWidth = 20
	}

	var b strings.Builder
	for _, e := range entries {
		lines := wrapText(e.desc, descWidth)
		for i, line := range lines {
			b.WriteString(indent)
			if i == 0 {
				b.WriteString(e.head)
				b.WriteString(strings.Repeat(" ", width-len(e.head)))
			} else {
				b.WriteString(strings.Repeat(" ", width))
			}
			b.WriteString("  ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return b.String()
}

// boolFlagValue - интерфейс, который реализуют значения булевых флагов
// в стандартном пакете flag (через встроенный, но экспортируемый метод
// IsBoolFlag). Используется, чтобы отличить булевы флаги от остальных
// без обращения к неэкспортируемым типам пакета flag.
type boolFlagValue interface {
	flag.Value
	IsBoolFlag() bool
}

// defaultSuffix возвращает "(default X)"/"(default \"X\")" для флага f,
// если значение по умолчанию стоит показывать: непустое и не "false" для
// булевых флагов. Строковые значения (name == "string") заключаются
// в кавычки, как принято в справке go-инструментов.
func defaultSuffix(f *flag.Flag, name string) string {
	if f.DefValue == "" {
		return ""
	}
	if fv, ok := f.Value.(boolFlagValue); ok && fv.IsBoolFlag() && f.DefValue == "false" {
		return ""
	}

	value := f.DefValue
	if name == "string" {
		value = fmt.Sprintf("%q", f.DefValue)
	}
	return i18n.T(
		fmt.Sprintf("(default %s)", value),
		fmt.Sprintf("(по умолчанию %s)", value),
	)
}

// wrapText разбивает s на строки не длиннее width символов (в рунах),
// перенося по границам слов. Пустая строка возвращает один пустой элемент.
func wrapText(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{""}
	}
	lines := make([]string, 0, 1)
	line := words[0]
	lineLen := utf8.RuneCountInString(line)
	for _, w := range words[1:] {
		wLen := utf8.RuneCountInString(w)
		if lineLen+1+wLen > width {
			lines = append(lines, line)
			line = w
			lineLen = wLen
			continue
		}
		line += " " + w
		lineLen += 1 + wLen
	}
	lines = append(lines, line)
	return lines
}
