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
	opts, err := parseIndexArgs(args)
	if err != nil {
		return err
	}

	root, err := filepath.Abs(opts.Path)
	if err != nil {
		return err
	}

	dbPath, err := dbpath.Find(opts.DB)
	if errors.Is(err, dbpath.ErrNotFound) {
		// Для index отсутствие базы - не ошибка, создаём новую в текущей
		// директории. Именно в текущей, а не в индексируемом пути: базу
		// ищут обходом вверх, поэтому созданная внутри подкаталога она
		// была бы не видна из корня проекта.
		wd, wdErr := os.Getwd()
		if wdErr != nil {
			return wdErr
		}
		dbPath, err = dbpath.Create(opts.DB, wd)
	}
	if err != nil {
		return err
	}

	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()

	if err := s.CheckSchema(); err != nil {
		return err
	}

	// Если meta ещё нет (свежая база), схема не создана и размерность
	// векторов неизвестна - она станет известна после первого эмбеддинга.
	// Проверка совпадения модели нужна только при --embed: без него
	// модель в базе не используется вообще.
	fresh := true
	if existingModel, existingDim, metaErr := s.Meta(); metaErr == nil {
		fresh = false
		if opts.Embed && existingModel != "" && existingModel != opts.Model {
			return fmt.Errorf(i18n.T("index was built with model %s (dim %d); remove .senso/index.db or specify --model %s", "индекс построен моделью %s (dim %d); удалите .senso/index.db или укажите --model %s"), existingModel, existingDim, existingModel)
		}
	}

	// Префиксы для эмбеддингов сохраняем в meta безусловно (в том числе
	// пустые), чтобы повторная индексация с новыми префиксами замещала
	// старые значения. Пишем сразу, если схема уже существует (!fresh);
	// для свежей базы это делается ниже, после первого s.Init.
	if opts.Embed && !fresh {
		if err := savePrefixes(s, opts); err != nil {
			return err
		}
	}

	// Без --embed индексация полностью локальная: схему создаём сразу,
	// не дожидаясь эмбеддинга (которого не будет), к Ollama вообще не
	// обращаемся.
	if fresh && !opts.Embed {
		if err := s.Init("", 0, root); err != nil {
			return err
		}
		fresh = false
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

	var client *embed.Client
	if opts.Embed {
		client = embed.New(opts.Ollama, opts.Model)
	}

	// backfill означает, что запрошены эмбеддинги, а векторов в базе пока
	// нет (индекс раньше строился без --embed) - тогда быстрый путь по
	// mtime/size отключается на этот запуск, чтобы все файлы прошли через
	// эмбеддинг. См. комментарий к applyBackfill.
	var backfill bool
	if opts.Embed && !fresh {
		hasVectors, err := s.HasVectors()
		if err != nil {
			return err
		}
		backfill = !hasVectors
	}

	// ctx отслеживает только Ctrl+C/SIGTERM между файлами: текущий файл
	// всегда докатывается до конца, чтобы не оставить индекс в
	// промежуточном состоянии.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	cwd, _ := os.Getwd()
	start := time.Now()
	// Значение флага уже проверено при разборе аргументов.
	strategy, _ := chunk.ParseStrategy(opts.Chunker)

	for i, path := range candidates {
		if ctx.Err() != nil {
			rep.Interrupted = true
			break
		}

		info, err := os.Stat(path)
		if err != nil {
			// файл исчез между сканированием и обработкой - не считаем
			// это ошибкой всей команды.
			rep.addSkip(skipVanished)
			continue
		}
		curMtime := info.ModTime().UnixNano()
		curSize := info.Size()

		var dbMtime, dbSize int64
		var dbHash string
		var found bool
		if !fresh {
			dbMtime, dbSize, dbHash, found, err = s.FileState(path)
			if err != nil {
				return err
			}
		}

		// Быстрый путь: метаданные совпали, содержимое читать не нужно.
		// При дозаполнении векторов (backfill) быстрый путь отключаем -
		// иначе файлы с неизменившимся содержимым никогда не попадут на
		// эмбеддинг.
		if found && curMtime == dbMtime && curSize == dbSize && !backfill {
			rep.Unchanged++
			continue
		}

		content, skip, err := readIndexable(path, int64(opts.MaxFileSize)*1024*1024)
		if err != nil {
			// Ошибка одного файла не прерывает индексацию: она попадает
			// в отчёт, а на код возврата влияет только при --strict.
			rep.addFailure(path, failureCode(err), err)
			continue
		}
		if skip != "" {
			rep.addSkip(skip)
			continue
		}
		curHash := hashContent(content)

		action := applyBackfill(decideFile(dbMtime, dbSize, dbHash, curMtime, curSize, curHash), backfill)
		switch action {
		case actionSkip:
			rep.Unchanged++
			continue
		case actionTouch:
			// Содержимое то же, обновляются только mtime и размер.
			if err := s.TouchFile(path, curMtime, curSize); err != nil {
				return err
			}
			rep.Unchanged++
			continue
		}

		// actionReindex.
		chunks := chunk.SplitFile(path, string(content), opts.ChunkSize, opts.Overlap, strategy)

		var vectors [][]float32
		if opts.Embed && len(chunks) > 0 {
			// Используем фоновый контекст, а не ctx: если Ctrl+C нажали
			// во время обработки этого файла, он всё равно должен
			// завершиться корректно, а прерывание случится перед
			// следующим файлом.
			vectors, err = embedAll(context.Background(), client, chunkTexts(chunks), opts)
			if err != nil {
				return err
			}

			if fresh {
				dim := len(vectors[0])
				if err := s.Init(opts.Model, dim, root); err != nil {
					return err
				}
				fresh = false
				if err := savePrefixes(s, opts); err != nil {
					return err
				}
			}
		} else if fresh {
			// Пустой набор чанков ничего не говорит о размерности
			// модели, а без схемы сохранить файл нельзя - пропускаем.
			// (fresh здесь возможен только при --embed: без него схема
			// уже создана до сканирования файлов.)
			rep.addSkip(skipNoSchema)
			continue
		}

		if err := s.ReplaceFile(path, curMtime, curSize, curHash, chunks, vectors); err != nil {
			return err
		}
		if found {
			rep.Updated++
		} else {
			rep.Indexed++
		}
		rep.Chunks += len(chunks)

		if !opts.Quiet {
			fmt.Fprintf(os.Stderr, "[%d/%d] %s (%d chunks)\n", i+1, len(candidates), shortenPath(path, cwd), len(chunks))
		}
	}

	// При прерывании индекс остаётся консистентным, но неполным: не
	// отмечаем время индексации и не подчищаем удалённые файлы, иначе
	// незатронутые записи выглядели бы как проверенные.
	if !rep.Interrupted && !fresh {
		if opts.Prune {
			rep.Deleted, err = pruneMissing(s, root)
			if err != nil {
				return err
			}
		}
		if err := s.SetIndexedAt(time.Now()); err != nil {
			return err
		}
		if err := s.AddRoot(root); err != nil {
			return err
		}
	}

	rep.DurationMS = time.Since(start).Milliseconds()

	if !opts.Quiet {
		printIndexSummary(os.Stderr, rep, cwd)
	}
	if opts.ReportJSON {
		if err := printIndexReportJSON(os.Stdout, rep); err != nil {
			return err
		}
	}

	return reportExitError(rep, opts.Strict)
}

// savePrefixes сохраняет в meta префиксы запроса и документа, заданные при
// индексации, чтобы "senso search --semantic" мог применять их
// автоматически, без повторного указания пользователем.
func savePrefixes(s *store.Store, opts indexOptions) error {
	if err := s.SetMeta("query_prefix", opts.QueryPrefix); err != nil {
		return err
	}
	return s.SetMeta("doc_prefix", opts.DocPrefix)
}

// pruneMissing удаляет из индекса файлы поддерева root, отсутствующие на
// диске. Файлы других корней, хранящиеся в той же базе, не затрагиваются.
func pruneMissing(s *store.Store, root string) (int, error) {
	paths, err := s.ListPaths("")
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

	return s.DeleteFiles(missing)
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
