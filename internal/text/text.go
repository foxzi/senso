// Package text отвечает за приведение содержимого файла к UTF-8 в форме NFC.
//
// Задача пакета: отличить текстовый файл от бинарного и, если файл записан
// в однобайтовой кириллической кодировке, распознать её и перекодировать.
// В индексе хранится только UTF-8; исходная кодировка нигде не сохраняется.
package text

import (
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/unicode/norm"
)

// SniffLen - размер префикса файла, по которому принимается решение
// о бинарности. Совпадает с эвристикой git.
const SniffLen = 8192

// minScore - минимальная оценка правдоподобия для однобайтовой кодировки.
// Ниже этого порога файл считается бинарным, а не текстом в неизвестной кодировке.
const minScore = 0.5

// Порог применимости проверки на пробелы и минимальная доля пробельных
// символов в связном тексте. В русской прозе пробел приходится примерно
// на каждый седьмой символ, поэтому запас до 2% многократный.
const (
	minWordyLen   = 32
	minSpaceShare = 0.02
)

// Encoding - распознанная кодировка исходного файла.
type Encoding string

const (
	UTF8        Encoding = "utf-8"
	Windows1251 Encoding = "windows-1251"
	KOI8R       Encoding = "koi8-r"
	CP866       Encoding = "ibm866"
)

// candidates перечисляет однобайтовые кодировки в порядке убывания
// распространённости. Порядок важен только для разрешения ничьих.
var candidates = []struct {
	name Encoding
	enc  encoding.Encoding
}{
	{Windows1251, charmap.Windows1251},
	{KOI8R, charmap.KOI8R},
	{CP866, charmap.CodePage866},
}

// IsBinary сообщает, содержит ли префикс данных нулевой байт.
// Нулевой байт - надёжный признак бинарного формата: в любой из
// поддерживаемых текстовых кодировок он не встречается.
func IsBinary(data []byte) bool {
	head := data
	if len(head) > SniffLen {
		head = head[:SniffLen]
	}
	for _, b := range head {
		if b == 0 {
			return true
		}
	}
	return false
}

// Decode приводит содержимое файла к UTF-8 в форме NFC.
//
// Порядок решений:
//  1. нулевой байт в префиксе - файл бинарный;
//  2. валидный UTF-8 - используется как есть;
//  3. иначе подбирается однобайтовая кириллическая кодировка;
//  4. если ни одна не набрала минимальной оценки - файл бинарный.
//
// Второе возвращаемое значение - false, если файл не является текстом.
func Decode(data []byte) (string, Encoding, bool) {
	if IsBinary(data) {
		return "", "", false
	}
	if utf8.Valid(data) {
		return Normalize(string(data)), UTF8, true
	}

	bestScore := 0.0
	bestText := ""
	bestEnc := Encoding("")
	for _, c := range candidates {
		decoded, err := c.enc.NewDecoder().Bytes(data)
		if err != nil {
			continue
		}
		s := scoreCyrillic(string(decoded))
		if s > bestScore {
			bestScore, bestText, bestEnc = s, string(decoded), c.name
		}
	}
	if bestScore < minScore {
		return "", "", false
	}
	return Normalize(bestText), bestEnc, true
}

// Normalize приводит строку к канонической форме NFC.
//
// Без этого шага визуально одинаковые строки могут иметь разное байтовое
// представление ("й" как один рунический код или как "и" + U+0306), что ломает
// и точное сравнение путей, и токенизацию на стороне модели эмбеддингов.
func Normalize(s string) string {
	if norm.NFC.IsNormalString(s) {
		return s
	}
	return norm.NFC.String(s)
}

// scoreCyrillic оценивает правдоподобие того, что строка - осмысленный
// кириллический текст. Результат в диапазоне [0, 1].
//
// Оценка строится на двух наблюдениях:
//
// Во-первых, при верной кодировке почти все байты старшей половины таблицы
// превращаются в кириллические буквы. Скажем, текст CP866, прочитанный как
// CP1251, даёт в диапазоне 0x80-0x9F типографские значки, а не буквы.
//
// Во-вторых, в естественном тексте строчные буквы преобладают над прописными.
// Это единственный признак, различающий CP1251 и KOI8-R: в этих кодировках
// диапазоны строчных и прописных букв зеркальны, поэтому текст KOI8-R,
// прочитанный как CP1251, выглядит как "пРИВЕТ" - кириллица распознаётся,
// но регистр вывернут наизнанку.
func scoreCyrillic(s string) float64 {
	var total, high, cyr, lower, bad, spaces int
	for _, r := range s {
		total++
		if unicode.IsSpace(r) {
			spaces++
		}
		switch {
		case r == utf8.RuneError:
			// Байт, которому в таблице кодировки не сопоставлен символ.
			bad++
			high++
		case r < utf8.RuneSelf:
			// ASCII встречается в любой кодировке и ничего не различает.
			continue
		default:
			high++
			if unicode.Is(unicode.Cyrillic, r) {
				cyr++
				if unicode.IsLower(r) {
					lower++
				}
			}
		}
	}
	if high == 0 {
		return 0
	}

	// Связный текст разбит на слова. Отсутствие пробелов на протяжении
	// десятков символов означает, что перед нами не проза, а двоичные данные,
	// случайно разложившиеся в буквы. Проверка безопасна: сюда попадает только
	// кириллица, а языки без пробельного деления слов кодируются в UTF-8
	// и разбираются веткой выше.
	if total >= minWordyLen && float64(spaces)/float64(total) < minSpaceShare {
		return 0
	}

	cyrRatio := float64(cyr) / float64(high)
	lowerShare := 0.0
	if cyr > 0 {
		lowerShare = float64(lower) / float64(cyr)
	}

	score := cyrRatio * (0.3 + 0.7*lowerShare)
	score -= float64(bad) / float64(high)
	if score < 0 {
		return 0
	}
	return score
}
