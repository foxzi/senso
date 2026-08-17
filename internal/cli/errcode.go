package cli

import (
	"errors"
)

// Стабильные коды ошибок для машинного использования. Код - контракт:
// текст сообщения может меняться и переводится на язык пользователя, код
// не меняется. Сейчас коды печатает только search в формате json-v2
// (см. printSearchErrorJSONV2).
const (
	// errCodeUsage - ошибка в аргументах команды (код выхода 2).
	errCodeUsage = "usage"
	// errCodeNoIndex - база senso не найдена.
	errCodeNoIndex = "no_index"
	// errCodeNoVectors - в базе нет векторов, семантический поиск
	// невозможен.
	errCodeNoVectors = "no_vectors"
	// errCodeEmbedFailed - не удалось получить эмбеддинг запроса от Ollama.
	errCodeEmbedFailed = "embed_failed"
	// errCodeInternal - остальные ошибки: сбой SQLite, повреждённая база,
	// ошибка ввода-вывода. Разбирать такую ошибку машине нечего, её
	// сообщение предназначено человеку.
	errCodeInternal = "internal"
)

// CodedError - ошибка с машиночитаемым кодом. Код проставляется там, где
// причина известна точно, а не угадывается по тексту сообщения.
type CodedError struct {
	Code string
	Err  error
}

func (e *CodedError) Error() string {
	return e.Err.Error()
}

func (e *CodedError) Unwrap() error {
	return e.Err
}

// withCode помечает ошибку машиночитаемым кодом, сохраняя её сообщение и
// цепочку обёрток. nil остаётся nil, чтобы вызов можно было писать без
// проверки.
func withCode(code string, err error) error {
	if err == nil {
		return nil
	}
	return &CodedError{Code: code, Err: err}
}

// errorCode возвращает код ошибки для машинного вывода. Сначала берётся
// явно проставленный код (withCode), затем распознаётся ошибка в
// аргументах; всё остальное считается внутренним сбоем.
func errorCode(err error) string {
	if err == nil {
		return ""
	}

	var coded *CodedError
	if errors.As(err, &coded) {
		return coded.Code
	}

	var usageErr *UsageError
	if errors.As(err, &usageErr) {
		return errCodeUsage
	}

	return errCodeInternal
}
