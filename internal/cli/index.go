package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"senso/internal/chunk"
	"senso/internal/dbpath"
	"senso/internal/embed"
	"senso/internal/i18n"
	"senso/internal/store"
	"senso/internal/walk"
)

// embedBatchSize - максимальное число чанков в одном запросе эмбеддинга.
const embedBatchSize = 32

// RunIndex реализует подкоманду "index": строит или обновляет индекс для
// указанного пути.
func RunIndex(args []string) error {
	// dbCtx - контекст для операций с базой данных: они намеренно не
	// отменяются посреди обработки файла, текущий файл всегда
	// дописывается до конца (см. sigCtx ниже).
	dbCtx := context.Background()

	opts, err := parseIndexArgs(args)
	if err != nil {
		return err
	}

	root, err := filepath.Abs(opts.Path)
	if err != nil {
		return err
	}

	s, dbPath, err := openIndexStore(dbCtx, opts)
	if err != nil {
		return err
	}
	defer s.Close()

	fresh, err := prepareIndexStore(dbCtx, s, opts, root)
	if err != nil {
		return err
	}

	// Отчёт создаётся до обхода дерева: ошибки чтения каталогов и
	// .gitignore тоже должны попадать в него, а не теряться молча.
	rep := newIndexReport()
	rep.Database = dbPath
	rep.Vectors = opts.Embed

	candidates, err := scanFiles(root, opts, func(path string, walkErr error) {
		rep.addFailure(path, failWalk, walkErr)
	}, func(e walk.Exclusion) {
		rep.addExclude(e.Reason)
	})
	if err != nil {
		return err
	}
	rep.Scanned = len(candidates)

	ix, err := newIndexer(dbCtx, s, opts, rep, root, fresh)
	if err != nil {
		return err
	}

	// sigCtx отслеживает только Ctrl+C/SIGTERM между файлами: текущий
	// файл всегда докатывается до конца, чтобы не оставить индекс в
	// промежуточном состоянии.
	sigCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	start := time.Now()
	for i, path := range candidates {
		if sigCtx.Err() != nil {
			rep.Interrupted = true
			break
		}
		if err := ix.processFile(path, i+1, len(candidates)); err != nil {
			return err
		}
	}

	if err := ix.finish(dbCtx); err != nil {
		return err
	}

	rep.DurationMS = time.Since(start).Milliseconds()

	if !opts.Quiet {
		printIndexSummary(os.Stderr, rep, ix.cwd)
	}
	if opts.ReportJSON {
		if err := printIndexReportJSON(os.Stdout, rep); err != nil {
			return err
		}
	}

	return reportExitError(rep, opts.Strict)
}

// openIndexStore находит или создаёт файл базы и открывает его. Для index
// отсутствие базы - не ошибка: новая создаётся в текущей директории. Именно
// в текущей, а не в индексируемом пути: базу ищут обходом вверх, поэтому
// созданная внутри подкаталога она была бы не видна из корня проекта.
func openIndexStore(ctx context.Context, opts indexOptions) (*store.Store, string, error) {
	dbPath, err := dbpath.Find(opts.DB)
	if errors.Is(err, dbpath.ErrNotFound) {
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return nil, "", wdErr
		}
		dbPath, err = dbpath.Create(opts.DB, wd)
	}
	if err != nil {
		return nil, "", err
	}

	s, err := store.Open(ctx, dbPath)
	if err != nil {
		return nil, "", err
	}
	if err := s.CheckSchema(ctx); err != nil {
		s.Close()
		return nil, "", err
	}
	return s, dbPath, nil
}

// prepareIndexStore сверяет параметры базы с параметрами запуска и создаёт
// схему, когда это возможно сделать до обхода файлов. Возвращает fresh=true,
// если схемы ещё нет (свежая база с --embed: размерность векторов станет
// известна только после первого эмбеддинга).
func prepareIndexStore(ctx context.Context, s *store.Store, opts indexOptions, root string) (bool, error) {
	// Если meta ещё нет (свежая база), схема не создана. Проверка
	// совпадения модели нужна только при --embed: без него модель в базе
	// не используется вообще.
	fresh := true
	if existingModel, existingDim, metaErr := s.Meta(ctx); metaErr == nil {
		fresh = false
		if opts.Embed && existingModel != "" && existingModel != opts.Model {
			return false, fmt.Errorf(i18n.T("index was built with model %s (dim %d); remove .senso/index.db or specify --model %s", "индекс построен моделью %s (dim %d); удалите .senso/index.db или укажите --model %s"), existingModel, existingDim, existingModel)
		}
	}

	// Параметры нарезки сверяем до первой записи: продолжать индексацию
	// с другой нарезкой нельзя, иначе база смешает чанки двух стратегий.
	if !fresh {
		if err := ensureChunkParams(ctx, s, opts); err != nil {
			return false, err
		}
	}

	// Префиксы для эмбеддингов сохраняем в meta безусловно (в том числе
	// пустые), чтобы повторная индексация с новыми префиксами замещала
	// старые значения. Пишем сразу, если схема уже существует (!fresh);
	// для свежей базы это делается после первого s.Init.
	if opts.Embed && !fresh {
		if err := savePrefixes(ctx, s, opts); err != nil {
			return false, err
		}
	}

	// Без --embed индексация полностью локальная: схему создаём сразу,
	// не дожидаясь эмбеддинга (которого не будет), к Ollama вообще не
	// обращаемся.
	if fresh && !opts.Embed {
		if err := s.Init(ctx, "", 0, root); err != nil {
			return false, err
		}
		fresh = false
		if err := saveIndexParams(ctx, s, opts); err != nil {
			return false, err
		}
	}

	return fresh, nil
}

// indexer хранит состояние обработки файлов между итерациями цикла
// индексации: подключение к базе, отчёт и флаги, меняющиеся по ходу работы
// (fresh, backfill).
type indexer struct {
	// ctx - контекст для операций с базой данных, отдельный от
	// сигнального контекста цикла индексации: текущий файл всегда
	// дописывается до конца, даже если пришёл Ctrl+C.
	ctx      context.Context
	s        *store.Store
	opts     indexOptions
	rep      *indexReport
	client   *embed.Client
	strategy chunk.Strategy
	root     string
	cwd      string
	fresh    bool
	backfill bool
}

// newIndexer готовит состояние цикла индексации: клиент эмбеддингов,
// стратегию нарезки и режим дозаполнения векторов.
func newIndexer(ctx context.Context, s *store.Store, opts indexOptions, rep *indexReport, root string, fresh bool) (*indexer, error) {
	ix := &indexer{ctx: ctx, s: s, opts: opts, rep: rep, root: root, fresh: fresh}
	ix.cwd, _ = os.Getwd()
	// Значение флага уже проверено при разборе аргументов.
	ix.strategy, _ = chunk.ParseStrategy(opts.Chunker)

	if opts.Embed {
		ix.client = embed.New(opts.Ollama, opts.Model)
	}

	// backfill означает, что запрошены эмбеддинги, а векторов в базе пока
	// нет (индекс раньше строился без --embed) - тогда быстрый путь по
	// mtime/size отключается на этот запуск, чтобы все файлы прошли через
	// эмбеддинг. См. комментарий к applyBackfill.
	if opts.Embed && !fresh {
		hasVectors, err := s.HasVectors(ctx)
		if err != nil {
			return nil, err
		}
		ix.backfill = !hasVectors
	}
	return ix, nil
}

// processFile обрабатывает один файл-кандидат: решает, нужна ли
// переиндексация, нарезает на чанки, при необходимости получает эмбеддинги и
// записывает результат. Ошибки чтения одного файла попадают в отчёт и не
// прерывают индексацию; ошибка возвращается только при отказе базы или
// сервиса эмбеддингов.
func (ix *indexer) processFile(path string, seq, total int) error {
	info, err := os.Stat(path)
	if err != nil {
		// файл исчез между сканированием и обработкой - не считаем
		// это ошибкой всей команды.
		ix.rep.addSkip(skipVanished)
		return nil
	}
	curMtime := info.ModTime().UnixNano()
	curSize := info.Size()

	var dbMtime, dbSize int64
	var dbHash string
	var found bool
	if !ix.fresh {
		dbMtime, dbSize, dbHash, found, err = ix.s.FileState(ix.ctx, path)
		if err != nil {
			return err
		}
	}

	// Быстрый путь: метаданные совпали, содержимое читать не нужно.
	// При дозаполнении векторов (backfill) быстрый путь отключаем -
	// иначе файлы с неизменившимся содержимым никогда не попадут на
	// эмбеддинг.
	if found && curMtime == dbMtime && curSize == dbSize && !ix.backfill {
		ix.rep.Unchanged++
		return nil
	}

	content, skip, err := readIndexable(path, int64(ix.opts.MaxFileSize)*1024*1024)
	if err != nil {
		// Ошибка одного файла не прерывает индексацию: она попадает
		// в отчёт, а на код возврата влияет только при --strict.
		ix.rep.addFailure(path, failureCode(err), err)
		return nil
	}
	if skip != "" {
		ix.rep.addSkip(skip)
		return nil
	}
	curHash := hashContent(content)

	action := applyBackfill(decideFile(dbMtime, dbSize, dbHash, curMtime, curSize, curHash), ix.backfill)
	switch action {
	case actionSkip:
		ix.rep.Unchanged++
		return nil
	case actionTouch:
		// Содержимое то же, обновляются только mtime и размер.
		if err := ix.s.TouchFile(ix.ctx, path, curMtime, curSize); err != nil {
			return err
		}
		ix.rep.Unchanged++
		return nil
	}

	// actionReindex.
	chunks := chunk.SplitFile(path, string(content), ix.opts.ChunkSize, ix.opts.Overlap, ix.strategy)

	var vectors [][]float32
	if ix.opts.Embed && len(chunks) > 0 {
		// Используем фоновый контекст, а не контекст сигналов: если
		// Ctrl+C нажали во время обработки этого файла, он всё равно
		// должен завершиться корректно, а прерывание случится перед
		// следующим файлом.
		vectors, err = embedAll(context.Background(), ix.client, chunkTexts(chunks), ix.opts)
		if err != nil {
			return err
		}

		if ix.fresh {
			if err := ix.initSchema(len(vectors[0])); err != nil {
				return err
			}
		}
	} else if ix.fresh {
		// Пустой набор чанков ничего не говорит о размерности
		// модели, а без схемы сохранить файл нельзя - пропускаем.
		// (fresh здесь возможен только при --embed: без него схема
		// уже создана до сканирования файлов.)
		ix.rep.addSkip(skipNoSchema)
		return nil
	}

	if err := ix.s.ReplaceFile(ix.ctx, path, curMtime, curSize, curHash, chunks, vectors); err != nil {
		return err
	}
	if found {
		ix.rep.Updated++
	} else {
		ix.rep.Indexed++
	}
	ix.rep.Chunks += len(chunks)

	if !ix.opts.Quiet {
		fmt.Fprintf(os.Stderr, "[%d/%d] %s (%d chunks)\n", seq, total, shortenPath(path, ix.cwd), len(chunks))
	}
	return nil
}

// initSchema создаёт схему свежей базы, когда размерность векторов стала
// известна после первого эмбеддинга, и записывает параметры индексации.
func (ix *indexer) initSchema(dim int) error {
	if err := ix.s.Init(ix.ctx, ix.opts.Model, dim, ix.root); err != nil {
		return err
	}
	ix.fresh = false
	if err := saveIndexParams(ix.ctx, ix.s, ix.opts); err != nil {
		return err
	}
	return savePrefixes(ix.ctx, ix.s, ix.opts)
}

// finish завершает индексацию: подчищает удалённые файлы (--prune) и
// отмечает время и корень. При прерывании индекс остаётся консистентным, но
// неполным: время индексации не отмечается и удалённые файлы не подчищаются,
// иначе незатронутые записи выглядели бы как проверенные.
func (ix *indexer) finish(ctx context.Context) error {
	if ix.rep.Interrupted || ix.fresh {
		return nil
	}
	if ix.opts.Prune {
		var err error
		ix.rep.Deleted, err = pruneMissing(ctx, ix.s, ix.root)
		if err != nil {
			return err
		}
	}
	if err := ix.s.SetIndexedAt(ctx, time.Now()); err != nil {
		return err
	}
	return ix.s.AddRoot(ctx, ix.root)
}

// savePrefixes сохраняет в meta префиксы запроса и документа, заданные при
// индексации, чтобы "senso search --semantic" мог применять их
// автоматически, без повторного указания пользователем.
func savePrefixes(ctx context.Context, s *store.Store, opts indexOptions) error {
	if err := s.SetMeta(ctx, "query_prefix", opts.QueryPrefix); err != nil {
		return err
	}
	return s.SetMeta(ctx, "doc_prefix", opts.DocPrefix)
}

// pruneMissing удаляет из индекса файлы поддерева root, отсутствующие на
// диске. Файлы других корней, хранящиеся в той же базе, не затрагиваются.
func pruneMissing(ctx context.Context, s *store.Store, root string) (int, error) {
	paths, err := s.ListPaths(ctx, "")
	if err != nil {
		return 0, err
	}

	var missing []string
	for _, p := range paths {
		if !prefixInSubtree(p, root) {
			continue
		}
		if _, err := os.Stat(p); os.IsNotExist(err) {
			missing = append(missing, p)
		}
	}
	if len(missing) == 0 {
		return 0, nil
	}

	return s.DeleteFiles(ctx, missing)
}

// chunkTexts извлекает текст из каждого чанка - embedAll работает с текстом,
// а номера строк для эмбеддинга не нужны.
func chunkTexts(chunks []chunk.Chunk) []string {
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	return texts
}

// embedAll получает эмбеддинги для всех чанков одного файла: разбивает их
// на батчи по embedBatchSize штук и обрабатывает до opts.Concurrency
// батчей параллельно. Результат нормализован по L2 и сохраняет порядок
// исходных чанков.
func embedAll(ctx context.Context, client *embed.Client, chunks []string, opts indexOptions) ([][]float32, error) {
	prefixed := make([]string, len(chunks))
	for i, c := range chunks {
		prefixed[i] = opts.DocPrefix + c
	}
	batches := batchStrings(prefixed, embedBatchSize)

	results := make([][][]float32, len(batches))
	errs := make([]error, len(batches))

	sem := make(chan struct{}, opts.Concurrency)
	var wg sync.WaitGroup
	for i, batch := range batches {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, batch []string) {
			defer wg.Done()
			defer func() { <-sem }()
			vecs, err := client.Embed(ctx, batch)
			if err != nil {
				errs[i] = err
				return
			}
			results[i] = vecs
		}(i, batch)
	}
	wg.Wait()

	for _, err := range errs {
		if err != nil {
			return nil, fmt.Errorf(i18n.T("failed to get embeddings from ollama (%s, model %s): %w", "не удалось получить эмбеддинги от Ollama (%s, модель %s): %w"), opts.Ollama, opts.Model, err)
		}
	}

	vectors := make([][]float32, 0, len(prefixed))
	for i, vecs := range results {
		if len(vecs) != len(batches[i]) {
			return nil, fmt.Errorf(i18n.T("ollama returned %d vectors for %d texts", "Ollama вернула %d векторов для %d текстов"), len(vecs), len(batches[i]))
		}
		for _, v := range vecs {
			embed.Normalize(v)
			vectors = append(vectors, v)
		}
	}
	return vectors, nil
}

// batchStrings разбивает items на последовательные подсрезы длиной не
// более size элементов, сохраняя порядок.
func batchStrings(items []string, size int) [][]string {
	if len(items) == 0 {
		return nil
	}
	var batches [][]string
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		batches = append(batches, items[i:end])
	}
	return batches
}
