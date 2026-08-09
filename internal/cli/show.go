package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"senso/internal/i18n"
	"senso/internal/store"
)

// showOptions хранит разобранные флаги и разобранную ссылку команды show.
type showOptions struct {
	Ref string

	DB     string
	Before int
	After  int
	JSON   bool

	// Path и Seq - путь файла и номер запрошенного чанка, разобранные из Ref.
	Path string
	Seq  int
}

// showFlagSet создаёт FlagSet подкоманды show, объявляет в opts все её
// флаги и возвращает FlagSet без вызова Parse. Используется как при разборе
// аргументов, так и при построении текста справки - это единственное место,
// где перечислены флаги show.
func showFlagSet(opts *showOptions) *flag.FlagSet {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.DB, "db", "", i18n.T("path to the database file", "путь к файлу базы данных"))
	fs.IntVar(&opts.Before, "before", 0, i18n.T("number of preceding chunks to include", "число предыдущих чанков, которые нужно включить"))
	fs.IntVar(&opts.After, "after", 0, i18n.T("number of following chunks to include", "число следующих чанков, которые нужно включить"))
	fs.BoolVar(&opts.JSON, "json", false, i18n.T("print the chunk as JSON", "вывести чанк в формате JSON"))
	return fs
}

// parseRef разбирает ссылку вида "<path>#<seq>" (в точности такую, какую
// печатает search в заголовке результата) на путь файла и номер чанка.
// Относительный путь приводится к абсолютному через filepath.Abs, потому
// что в индексе пути всегда абсолютные.
func parseRef(ref string) (path string, seq int, err error) {
	usage := usagef("%s", i18n.T(
		"reference must be in the form <path>#<chunk>, e.g. /docs/spec.docx#4",
		"ссылка должна быть в формате <путь>#<чанк>, например /docs/spec.docx#4",
	))

	i := strings.LastIndex(ref, "#")
	if i <= 0 || i == len(ref)-1 {
		return "", 0, usage
	}

	seq, convErr := strconv.Atoi(ref[i+1:])
	if convErr != nil || seq < 0 {
		return "", 0, usage
	}

	path, err = filepath.Abs(ref[:i])
	if err != nil {
		return "", 0, err
	}
	return path, seq, nil
}

// parseShowArgs разбирает аргументы командной строки подкоманды show.
func parseShowArgs(args []string) (showOptions, error) {
	var opts showOptions

	fs := showFlagSet(&opts)
	fs.Usage = func() {
		fmt.Fprint(fs.Output(), commandHelpText(showUsageLine(), showSummary(), fs))
	}
	if err := fs.Parse(args); err != nil {
		return showOptions{}, finishParse(fs, err)
	}
	if fs.NArg() != 1 {
		return showOptions{}, usagef("%s", i18n.T(
			"exactly one argument is required - the reference <path>#<chunk>",
			"требуется ровно один аргумент - ссылка <путь>#<чанк>",
		))
	}
	if opts.Before < 0 {
		return showOptions{}, usagef("%s", i18n.T("--before cannot be negative", "--before не может быть отрицательным"))
	}
	if opts.After < 0 {
		return showOptions{}, usagef("%s", i18n.T("--after cannot be negative", "--after не может быть отрицательным"))
	}

	opts.Ref = fs.Arg(0)
	path, seq, err := parseRef(opts.Ref)
	if err != nil {
		return showOptions{}, err
	}
	opts.Path = path
	opts.Seq = seq

	return opts, nil
}

// RunShow реализует подкоманду "show": читает из индекса сохранённый чанк
// по ссылке из выдачи search, вместе с --before/--after соседними чанками
// того же файла.
//
// Соседние чанки не склеиваются в один текст: каждый возвращается отдельным
// элементом, как он лежит в индексе. Соседние чанки в индексе перекрываются
// (см. internal/chunk) - склейка убрала бы это перекрытие только для
// границы между чанками N и N+1, а не для запрошенного окна в целом, что
// давало бы читателю ложное ощущение непрерывности текста. Отдельные
// элементы с видимым перекрытием проще реализовать и не вводят в
// заблуждение о том, что на самом деле хранится в индексе.
func RunShow(args []string) error {
	opts, err := parseShowArgs(args)
	if err != nil {
		return err
	}

	s, _, err := openStore(opts.DB)
	if err != nil {
		return err
	}
	defer s.Close()

	chunks, err := s.Chunks(opts.Path, opts.Seq-opts.Before, opts.Seq+opts.After)
	if err != nil {
		return err
	}
	if !hasSeq(chunks, opts.Seq) {
		return showChunkNotFoundError(s, opts.Path, opts.Seq)
	}

	// Проверка свежести не блокирует вывод: агенту полезнее получить
	// сохранённый в индексе текст с пометкой "stale", чем ошибку - решение
	// о переиндексации в любом случае принимает не эта команда, а senso
	// index. Поэтому расхождение только предупреждает через stderr и
	// отражается в JSON, код выхода остаётся 0.
	stale, reason, err := checkStale(opts.Path, s)
	if err != nil {
		return err
	}
	if stale {
		fmt.Fprintln(os.Stderr, staleWarning(opts.Path, reason))
	}

	if opts.JSON {
		return printShowJSON(opts.Path, opts.Seq, chunks, stale, reason)
	}
	printShowText(chunks)
	return nil
}

// hasSeq сообщает, есть ли среди chunks элемент с номером seq.
func hasSeq(chunks []store.Result, seq int) bool {
	for _, c := range chunks {
		if c.Seq == seq {
			return true
		}
	}
	return false
}

// showChunkNotFoundError строит ошибку об отсутствии запрошенного чанка.
// Если у файла чанки есть, в сообщении указывается их доступный диапазон;
// случай, когда файла нет в индексе вовсе, отсекается раньше - его ловит
// сама s.Chunks.
func showChunkNotFoundError(s *store.Store, path string, seq int) error {
	lo, hi, found, err := s.ChunkSeqRange(path)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf(i18n.T(
			"file %q has no chunks in the index",
			"у файла %q нет чанков в индексе",
		), path)
	}
	return fmt.Errorf(i18n.T(
		"file %q has no chunk #%d (available: #%d-#%d)",
		"у файла %q нет чанка #%d (доступны: #%d-#%d)",
	), path, seq, lo, hi)
}

// checkStale сравнивает текущие mtime и размер файла на диске с
// сохранёнными в индексе при последней индексации. mtime сравнивается в
// наносекундах, как его записывает senso index (info.ModTime().UnixNano()).
// reason - машиночитаемый код причины: "missing" (файл исчез с диска) или
// "modified" (mtime/size расходятся); при stale=false reason пуст.
func checkStale(path string, s *store.Store) (stale bool, reason string, err error) {
	dbMtime, dbSize, _, found, err := s.FileState(path)
	if err != nil {
		return false, "", err
	}
	if !found {
		// s.Chunks(path, ...) уже вернула бы ошибку "файл не найден" до
		// этого места - сюда попасть в норме нельзя, но на случай гонки
		// (файл удалили из индекса между двумя запросами) считаем это
		// пропажей файла, а не падаем.
		return true, "missing", nil
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		if os.IsNotExist(statErr) {
			return true, "missing", nil
		}
		return false, "", statErr
	}
	if info.ModTime().UnixNano() != dbMtime || info.Size() != dbSize {
		return true, "modified", nil
	}
	return false, "", nil
}

// staleWarning форматирует предупреждение о несвежем файле для stderr.
func staleWarning(path, reason string) string {
	switch reason {
	case "missing":
		return i18n.Tf(
			"warning: %s is no longer on disk; showing text saved in the index",
			"предупреждение: %s больше не на диске; показан текст, сохранённый в индексе",
			path,
		)
	default:
		return i18n.Tf(
			"warning: %s has changed since indexing; showing text saved in the index, run senso index to refresh",
			"предупреждение: %s изменился после индексации; показан текст, сохранённый в индексе, запустите senso index для обновления",
			path,
		)
	}
}

// printShowText печатает чанки в человекочитаемом виде: путь сокращается
// относительно текущей директории, для каждого чанка печатается заголовок
// (см. formatChunkRef) и следом полный текст без обрезки.
func printShowText(chunks []store.Result) {
	cwd, _ := os.Getwd()
	for i, c := range chunks {
		if i > 0 {
			fmt.Println()
		}
		path := shortenPath(c.Path, cwd)
		fmt.Println(formatChunkRef(path, c.Seq, c.StartLine, c.EndLine))
		fmt.Println(c.Text)
	}
}

// showChunkJSON - один чанк в JSON-выводе show.
type showChunkJSON struct {
	Ref       string `json:"ref"`
	Chunk     int    `json:"chunk"`
	StartLine int    `json:"line_start"`
	EndLine   int    `json:"line_end"`
	Text      string `json:"text"`
}

// showJSON - структура верхнего уровня JSON-вывода show.
type showJSON struct {
	Ref         string          `json:"ref"`
	Path        string          `json:"path"`
	Chunk       int             `json:"chunk"`
	Stale       bool            `json:"stale"`
	StaleReason string          `json:"stale_reason,omitempty"`
	Chunks      []showChunkJSON `json:"chunks"`
}

// printShowJSON печатает запрошенный чанк и его соседей как один
// JSON-объект. Текст не обрезается: в отличие от search --snippet, смысл
// show - отдать агенту (в частности, для форматов вроде docx/epub/pptx,
// где другого способа прочитать текст нет) полный сохранённый в индексе
// текст. Верхнее поле ref строится из канонического (абсолютного) пути и
// запрошенного номера чанка, а не из исходной строки аргумента - так ref
// в ответе можно тут же передать обратно в show без изменений, даже если
// пользователь указал относительный путь.
func printShowJSON(path string, seq int, chunks []store.Result, stale bool, reason string) error {
	out := showJSON{
		Ref:         fmt.Sprintf("%s#%d", path, seq),
		Path:        path,
		Chunk:       seq,
		Stale:       stale,
		StaleReason: reason,
		Chunks:      make([]showChunkJSON, 0, len(chunks)),
	}
	for _, c := range chunks {
		out.Chunks = append(out.Chunks, showChunkJSON{
			Ref:       fmt.Sprintf("%s#%d", c.Path, c.Seq),
			Chunk:     c.Seq,
			StartLine: c.StartLine,
			EndLine:   c.EndLine,
			Text:      c.Text,
		})
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}
