package chunk

import (
	"os"
	"strings"
	"testing"
)

// Проверка на реальных файлах: каждая непустая строка тела фрагмента
// (без хвоста перекрытия) должна лежать внутри объявленного диапазона.
func TestLineRangesMatchRealFiles(t *testing.T) {
	files := []string{"chunk.go", "../store/store.go", "../cli/search.go", "../text/text.go"}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		src := string(b)
		fileLines := strings.Split(src, "\n")
		for _, size := range []int{120, 400, 1500} {
			for _, ov := range []int{0, 40, 200} {
				for i, c := range Split(src, size, ov) {
					if c.StartLine < 1 || c.EndLine > len(fileLines) || c.EndLine < c.StartLine {
						t.Fatalf("%s size=%d ov=%d chunk %d: некорректный диапазон %d-%d (в файле %d строк)",
							f, size, ov, i, c.StartLine, c.EndLine, len(fileLines))
					}
					window := strings.Join(fileLines[c.StartLine-1:c.EndLine], "\n")
					for _, ln := range strings.Split(c.Text, "\n") {
						if strings.TrimSpace(ln) == "" {
							continue
						}
						if !strings.Contains(window, strings.TrimSpace(ln)) {
							t.Fatalf("%s size=%d ov=%d chunk %d (строки %d-%d): строка %q отсутствует в объявленном диапазоне",
								f, size, ov, i, c.StartLine, c.EndLine, ln)
						}
					}
				}
			}
		}
	}
}
