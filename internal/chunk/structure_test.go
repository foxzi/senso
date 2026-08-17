package chunk

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestParseStrategy(t *testing.T) {
	tests := []struct {
		in   string
		want Strategy
		ok   bool
	}{
		{"auto", Auto, true},
		{"text", Text, true},
		{"", "", false},
		{"AUTO", "", false},
		{"tree-sitter", "", false},
	}
	for _, tt := range tests {
		got, ok := ParseStrategy(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseStrategy(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

// boundaryLines переводит смещения границ в номера строк, чтобы ожидания
// в тестах не зависели от точной длины текста.
func boundaryLines(t *testing.T, path, text string) []int {
	t.Helper()
	nl := newlineOffsets(text)
	var out []int
	for _, off := range boundaries(path, text) {
		out = append(out, lineAt(nl, off))
	}
	return out
}

func TestBoundariesMarkdown(t *testing.T) {
	text := strings.Join([]string{
		"# Title",                // 1: первая строка, границей не считается
		"",                       // 2
		"intro",                  // 3
		"",                       // 4
		"## Section",             // 5: граница
		"",                       // 6
		"```md",                  // 7
		"# not really a heading", // 8: внутри блока кода
		"```",                    // 9
		"#nospace",               // 10: не заголовок
		"####### too deep",       // 11: больше шести уровней
		"### Deep",               // 12: граница
	}, "\n")

	got := boundaryLines(t, "readme.md", text)
	want := []int{5, 12}
	if !equalInts(got, want) {
		t.Errorf("границы Markdown = %v, want %v", got, want)
	}
}

func TestBoundariesGo(t *testing.T) {
	text := strings.Join([]string{
		"package main",          // 1
		"",                      // 2
		"import \"fmt\"",        // 3
		"",                      // 4
		"// Doc описывает Foo.", // 5: граница поднимается на комментарий
		"func Foo() {",          // 6
		"\tbar := func() {}",    // 7: с отступом - не верхний уровень
		"\t_ = bar",             // 8
		"}",                     // 9
		"",                      // 10
		"type T struct{}",       // 11: граница
		"",                      // 12
		"const K = 1",           // 13: граница
	}, "\n")

	got := boundaryLines(t, "main.go", text)
	want := []int{5, 11, 13}
	if !equalInts(got, want) {
		t.Errorf("границы Go = %v, want %v", got, want)
	}
}

func TestBoundariesPython(t *testing.T) {
	text := strings.Join([]string{
		"import os",          // 1
		"",                   // 2
		"class A:",           // 3: граница
		"    def one(self):", // 4: метод - тоже граница
		"        return 1",   // 5
		"",                   // 6
		"    @property",      // 7: граница поднимается на декоратор
		"    def two(self):", // 8
		"        return 2",   // 9
		"",                   // 10
		"async def main():",  // 11: граница
		"    pass",           // 12
	}, "\n")

	got := boundaryLines(t, "app.py", text)
	want := []int{3, 4, 7, 11}
	if !equalInts(got, want) {
		t.Errorf("границы Python = %v, want %v", got, want)
	}
}

func TestBoundariesJS(t *testing.T) {
	text := strings.Join([]string{
		"import x from 'x'",         // 1
		"",                          // 2
		"export function a() {",     // 3: граница
		"  const inner = () => {};", // 4: с отступом
		"  return inner;",           // 5
		"}",                         // 6
		"",                          // 7
		"class B {}",                // 8: граница
		"",                          // 9
		"const notADecl = 1",        // 10: не в списке префиксов
	}, "\n")

	got := boundaryLines(t, "app.ts", text)
	want := []int{3, 8}
	if !equalInts(got, want) {
		t.Errorf("границы JS/TS = %v, want %v", got, want)
	}
}

func TestBoundariesYAML(t *testing.T) {
	text := strings.Join([]string{
		"name: ci",      // 1
		"on:",           // 2: граница
		"  push:",       // 3: вложенный ключ
		"    branches:", // 4
		"# комментарий", // 5: граница блока jobs поднимается на комментарий
		"jobs:",         // 6
		"  build:",      // 7
		"---",           // 8: новый документ
		"other: 1",      // 9
	}, "\n")

	got := boundaryLines(t, "ci.yml", text)
	want := []int{2, 5, 8, 9}
	if !equalInts(got, want) {
		t.Errorf("границы YAML = %v, want %v", got, want)
	}
}

func TestBoundariesJSON(t *testing.T) {
	text := strings.Join([]string{
		"{",              // 1
		"  \"a\": 1,",    // 2: элемент верхнего уровня
		"  \"b\": {",     // 3: элемент верхнего уровня
		"    \"c\": 2",   // 4: вложенный
		"  },",           // 5: закрывающая скобка
		"  \"d\": \"}\"", // 6: скобка внутри строки не меняет глубину
		"}",              // 7
	}, "\n")

	got := boundaryLines(t, "data.json", text)
	want := []int{2, 3, 6}
	if !equalInts(got, want) {
		t.Errorf("границы JSON = %v, want %v", got, want)
	}
}

func TestBoundariesMinifiedJSON(t *testing.T) {
	if got := boundaries("data.json", `{"a":1,"b":{"c":2}}`); got != nil {
		t.Errorf("для файла в одну строку границ быть не должно, получено %v", got)
	}
}

func TestBoundariesUnknownExtension(t *testing.T) {
	text := "func Foo() {}\nfunc Bar() {}\n"
	if got := boundaries("main.unknown", text); got != nil {
		t.Errorf("для неизвестного расширения границ быть не должно, получено %v", got)
	}
}

func TestSplitFileTextStrategyMatchesSplit(t *testing.T) {
	text := strings.Join([]string{
		"# Title", "", strings.Repeat("intro ", 100),
		"", "## Section", "", strings.Repeat("body ", 100),
	}, "\n")

	want := Split(text, 200, 40)
	got := SplitFile("readme.md", text, 200, 40, Text)
	if len(got) != len(want) {
		t.Fatalf("стратегия text дала %d чанков, а Split - %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("чанк %d: стратегия text расходится с Split:\n%#v\n%#v", i, got[i], want[i])
		}
	}
}

func TestSplitFileStartsChunkAtHeading(t *testing.T) {
	body := strings.Repeat("текст раздела. ", 20)
	text := "# Заголовок\n" + body + "\n## Второй раздел\n" + body + "\n"

	chunks := SplitFile("doc.md", text, 200, 0, Auto)
	if len(chunks) < 2 {
		t.Fatalf("ожидалось не меньше двух чанков, получено %d", len(chunks))
	}
	found := false
	for _, c := range chunks {
		if strings.HasPrefix(c.Text, "## Второй раздел") {
			found = true
		}
	}
	if !found {
		t.Errorf("ни один чанк не начинается с заголовка раздела: %#v", chunks)
	}
}

func TestSplitFileKeepsShortBlocksTogether(t *testing.T) {
	// Восемь коротких функций должны уместиться в один чанк: структурная
	// граница не обязана начинать новый фрагмент, пока текущий почти пуст.
	var b strings.Builder
	for i := 0; i < 8; i++ {
		b.WriteString("func f() {}\n")
	}
	chunks := SplitFile("main.go", b.String(), 1200, 0, Auto)
	if len(chunks) != 1 {
		t.Errorf("ожидался один чанк, получено %d: %#v", len(chunks), chunks)
	}
}

func TestSplitFileRespectsChunkSize(t *testing.T) {
	// Одно объявление длиннее лимита обязано быть разрезано, несмотря на то
	// что структурно это единый блок.
	text := "package main\n\nfunc Big() {\n" + strings.Repeat("\tcall()\n", 200) + "}\n"
	for _, size := range []int{100, 300, 1200} {
		chunks := SplitFile("main.go", text, size, 0, Auto)
		for i, c := range chunks {
			if n := utf8.RuneCountInString(c.Text); n > size {
				t.Errorf("size=%d: чанк %d длиной %d рун превышает лимит", size, i, n)
			}
		}
	}
}

func TestSplitFileLineRangesStayCorrect(t *testing.T) {
	body := strings.Repeat("\tcall()\n", 30)
	text := "package main\n\nfunc A() {\n" + body + "}\n\nfunc B() {\n" + body + "}\n"
	lines := strings.Split(text, "\n")

	for _, size := range []int{80, 200, 500} {
		chunks := SplitFile("main.go", text, size, 20, Auto)
		for i, c := range chunks {
			if c.StartLine < 1 || c.EndLine < c.StartLine || c.EndLine > len(lines) {
				t.Fatalf("size=%d: чанк %d имеет некорректный диапазон %d-%d", size, i, c.StartLine, c.EndLine)
			}
			if i > 0 && c.StartLine < chunks[i-1].StartLine {
				t.Fatalf("size=%d: чанк %d начинается выше предыдущего", size, i)
			}
			// Первая строка текста чанка должна встречаться в объявленном
			// диапазоне строк файла.
			first := strings.TrimSpace(strings.SplitN(c.Text, "\n", 2)[0])
			if first == "" {
				continue
			}
			window := strings.Join(lines[c.StartLine-1:c.EndLine], "\n")
			if !strings.Contains(window, first) {
				t.Fatalf("size=%d: чанк %d (%d-%d) не найден в своём диапазоне: %q",
					size, i, c.StartLine, c.EndLine, first)
			}
		}
	}
}

func TestSplitFileDoesNotGlueLines(t *testing.T) {
	// Границы блоков склеиваются переводом строки: строки исходного файла не
	// должны срастаться в одну.
	text := "func a() {}\nfunc b() {}\nfunc c() {}\n"
	for _, c := range SplitFile("main.go", text, 1200, 0, Auto) {
		if strings.Contains(c.Text, "}func") {
			t.Errorf("строки склеились без перевода: %q", c.Text)
		}
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
