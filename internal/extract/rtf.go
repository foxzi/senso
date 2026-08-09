package extract

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// rtfGroup - состояние группы RTF. Группы вложены друг в друга, и каждая
// наследует состояние родителя: свойства, заданные внутри группы, действуют
// до её закрытия.
type rtfGroup struct {
	uc    int  // число символов-заменителей после \uN
	skip  bool // содержимое группы не является текстом документа
	first bool // очередное управляющее слово будет первым в группе
}

// rtfDestinations - управляющие слова, которые в начале группы объявляют
// её служебной: таблицы шрифтов и стилей, метаданные, картинки, инструкции
// полей. Текст таких групп в документ не входит.
var rtfDestinations = map[string]bool{
	"fonttbl": true, "colortbl": true, "stylesheet": true, "filetbl": true,
	"listtable": true, "listoverridetable": true, "revtbl": true, "rsidtbl": true,
	"info": true, "pict": true, "object": true, "themedata": true,
	"colorschememapping": true, "datastore": true, "latentstyles": true,
	"xmlnstbl": true, "generator": true, "fldinst": true,
}

// rtfCodepages - кодовые страницы, которые может объявить \ansicpg.
// По ним разбираются байты, записанные как \'hh.
var rtfCodepages = map[int]*charmap.Charmap{
	437: charmap.CodePage437, 850: charmap.CodePage850, 866: charmap.CodePage866,
	1250: charmap.Windows1250, 1251: charmap.Windows1251, 1252: charmap.Windows1252,
	1253: charmap.Windows1253, 1254: charmap.Windows1254, 1255: charmap.Windows1255,
	1256: charmap.Windows1256, 1257: charmap.Windows1257, 1258: charmap.Windows1258,
	10000: charmap.Macintosh,
}

// rtfParser разбирает поток RTF по байтам, отслеживая стек групп.
type rtfParser struct {
	data  []byte
	pos   int
	out   strings.Builder
	stack []rtfGroup
	page  *charmap.Charmap
	// pending - сколько ближайших символов нужно проглотить: это
	// заменители, которые идут следом за \uN для читателей, не знающих
	// Юникода.
	pending int
}

// rtf извлекает текст из документа Rich Text Format.
//
// Проверка заголовка намеренно мягкая: полноценный документ начинается
// с "{\rtf1", но встречаются и фрагменты без версии - им достаточно
// открывающей группы. Задача проверки - отсеять файлы, которые к RTF
// отношения не имеют.
func rtf(data []byte) (string, error) {
	head := data
	if len(head) > 16 {
		head = head[:16]
	}
	if !strings.HasPrefix(strings.TrimLeft(string(head), " \r\n\t"), `{\`) {
		return "", errors.New(`extract: not an rtf document (no opening group)`)
	}

	p := &rtfParser{
		data:  data,
		stack: []rtfGroup{{uc: 1, first: true}},
		page:  charmap.Windows1252,
	}
	p.parse()
	return strings.TrimSpace(p.out.String()), nil
}

// parse проходит документ от начала до конца.
func (p *rtfParser) parse() {
	for p.pos < len(p.data) {
		switch c := p.data[p.pos]; c {
		case '{':
			p.pos++
			g := *p.top()
			g.first = true
			p.stack = append(p.stack, g)
			// Заменители относятся только к тому месту, где встретилось
			// \uN: за границу группы они не переносятся.
			p.pending = 0
		case '}':
			p.pos++
			if len(p.stack) > 1 {
				p.stack = p.stack[:len(p.stack)-1]
			}
			p.pending = 0
		case '\\':
			p.control()
		case '\r', '\n':
			// Переводы строк в самом файле - оформление разметки,
			// разрывы абзацев задаются словом \par.
			p.pos++
		default:
			p.pos++
			p.emitByte(c)
		}
	}
}

// top возвращает текущую (самую вложенную) группу.
func (p *rtfParser) top() *rtfGroup {
	return &p.stack[len(p.stack)-1]
}

// control разбирает последовательность, начинающуюся с обратной косой черты:
// экранированный символ, шестнадцатеричный байт или управляющее слово
// с необязательным числовым параметром.
func (p *rtfParser) control() {
	p.pos++ // сама обратная косая черта
	if p.pos >= len(p.data) {
		return
	}

	switch c := p.data[p.pos]; c {
	case '\\', '{', '}':
		p.pos++
		p.emit(string(c))
		return
	case '\'':
		p.pos++
		if p.pos+2 <= len(p.data) {
			v, err := strconv.ParseUint(string(p.data[p.pos:p.pos+2]), 16, 8)
			p.pos += 2
			if err == nil {
				p.emitByte(byte(v))
			}
		}
		return
	case '*':
		// {\*\word ...} - расширение, неизвестное читателю; по спецификации
		// такую группу следует пропускать целиком.
		p.pos++
		p.top().skip = true
		return
	case '\r', '\n':
		p.pos++
		p.emit("\n")
		return
	case '~':
		p.pos++
		p.emit(" ")
		return
	case '-', '_', ':':
		// Мягкий перенос, неразрывный дефис, подпись указателя -
		// видимого текста не дают.
		p.pos++
		return
	}

	if !isASCIILetter(p.data[p.pos]) {
		p.pos++
		return
	}

	start := p.pos
	for p.pos < len(p.data) && isASCIILetter(p.data[p.pos]) {
		p.pos++
	}
	word := string(p.data[start:p.pos])

	numStart := p.pos
	if p.pos < len(p.data) && p.data[p.pos] == '-' {
		p.pos++
	}
	for p.pos < len(p.data) && isDigit(p.data[p.pos]) {
		p.pos++
	}
	param, hasParam := 0, p.pos > numStart
	if hasParam {
		param, _ = strconv.Atoi(string(p.data[numStart:p.pos]))
	}

	// Один пробел после управляющего слова - разделитель, а не текст.
	if p.pos < len(p.data) && p.data[p.pos] == ' ' {
		p.pos++
	}

	p.apply(word, param, hasParam)
}

// apply исполняет управляющее слово.
func (p *rtfParser) apply(word string, param int, hasParam bool) {
	first := p.top().first
	p.top().first = false

	switch word {
	case "par", "line", "sect", "page", "column", "row":
		p.emit("\n")
		return
	case "tab", "cell":
		p.emit("\t")
		return
	case "u":
		p.emitUnicode(param)
		return
	case "uc":
		if hasParam && param >= 0 {
			p.top().uc = param
		}
		return
	case "ansicpg":
		if cp, ok := rtfCodepages[param]; hasParam && ok {
			p.page = cp
		}
		return
	case "bin":
		// За словом следует param байт двоичных данных, которые
		// разбирать как разметку нельзя.
		if hasParam && param > 0 {
			p.pos += min(param, len(p.data)-p.pos)
		}
		return
	}

	if first && rtfDestinations[word] {
		p.top().skip = true
	}
}

// emitUnicode выводит символ, заданный словом \uN, и отмечает, что дальше
// идут символы-заменители, которые нужно проглотить.
func (p *rtfParser) emitUnicode(n int) {
	if p.top().skip {
		// Внутри служебной группы заменители считать не нужно: они
		// всё равно не попадут в текст, а счётчик протёк бы наружу.
		return
	}
	if n < 0 {
		// Значения больше 32767 записываются отрицательными числами.
		n += 65536
	}
	if n > 0 && n <= utf8.MaxRune {
		p.emit(string(rune(n)))
	}
	p.pending = p.top().uc
}

// emitByte выводит байт документа, разбирая его по текущей кодовой странице.
func (p *rtfParser) emitByte(b byte) {
	if b < utf8.RuneSelf {
		p.emit(string(rune(b)))
		return
	}
	p.emit(string(p.page.DecodeByte(b)))
}

// emit добавляет символ в результат, если текущая группа не служебная
// и символ не является заменителем после \uN.
func (p *rtfParser) emit(s string) {
	if p.top().skip {
		return
	}
	if p.pending > 0 {
		p.pending--
		return
	}
	p.out.WriteString(s)
}

func isASCIILetter(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

func isDigit(b byte) bool {
	return b >= '0' && b <= '9'
}
