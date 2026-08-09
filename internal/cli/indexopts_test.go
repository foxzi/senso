package cli

import (
	"testing"
)

func TestParseIndexArgsDefaults(t *testing.T) {
	opts, err := parseIndexArgs(nil)
	if err != nil {
		t.Fatalf("parseIndexArgs(nil) вернул ошибку: %v", err)
	}

	if opts.Path != "." {
		t.Errorf("Path = %q, хотим %q", opts.Path, ".")
	}
	if opts.DB != "" {
		t.Errorf("DB = %q, хотим пустую строку", opts.DB)
	}
	if opts.Model != "bge-m3" {
		t.Errorf("Model = %q, хотим %q", opts.Model, "bge-m3")
	}
	if opts.Ext != "" {
		t.Errorf("Ext = %q, хотим пустую строку", opts.Ext)
	}
	if opts.Exclude != "" {
		t.Errorf("Exclude = %q, хотим пустую строку", opts.Exclude)
	}
	if opts.NoGitignore != false {
		t.Errorf("NoGitignore = %v, хотим false", opts.NoGitignore)
	}
	if opts.ChunkSize != 1200 {
		t.Errorf("ChunkSize = %d, хотим 1200", opts.ChunkSize)
	}
	if opts.Overlap != 150 {
		t.Errorf("Overlap = %d, хотим 150", opts.Overlap)
	}
	if opts.QueryPrefix != "" {
		t.Errorf("QueryPrefix = %q, хотим пустую строку", opts.QueryPrefix)
	}
	if opts.DocPrefix != "" {
		t.Errorf("DocPrefix = %q, хотим пустую строку", opts.DocPrefix)
	}
	if opts.MaxFileSize != 10 {
		t.Errorf("MaxFileSize = %d, хотим 10", opts.MaxFileSize)
	}
	if opts.Concurrency != 4 {
		t.Errorf("Concurrency = %d, хотим 4", opts.Concurrency)
	}
	if opts.Prune != true {
		t.Errorf("Prune = %v, хотим true", opts.Prune)
	}
	if opts.Ollama == "" {
		t.Errorf("Ollama пустой, хотим значение по умолчанию")
	}
	if opts.Quiet != false {
		t.Errorf("Quiet = %v, хотим false", opts.Quiet)
	}
}

func TestParseIndexArgsPositional(t *testing.T) {
	opts, err := parseIndexArgs([]string{"/tmp/project"})
	if err != nil {
		t.Fatalf("parseIndexArgs вернул ошибку: %v", err)
	}
	if opts.Path != "/tmp/project" {
		t.Errorf("Path = %q, хотим %q", opts.Path, "/tmp/project")
	}
}

func TestParseIndexArgsTooManyPositional(t *testing.T) {
	_, err := parseIndexArgs([]string{"/tmp/one", "/tmp/two"})
	if err == nil {
		t.Fatal("ожидалась ошибка при двух позиционных аргументах")
	}
	if _, ok := err.(*UsageError); !ok {
		t.Errorf("ошибка не является *UsageError: %T", err)
	}
}

func TestParseIndexArgsValidation(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"chunk-size = 0", []string{"--chunk-size=0"}},
		{"chunk-size отрицательный", []string{"--chunk-size=-5"}},
		{"overlap отрицательный", []string{"--overlap=-1"}},
		{"overlap равен chunk-size", []string{"--chunk-size=100", "--overlap=100"}},
		{"overlap больше chunk-size", []string{"--chunk-size=100", "--overlap=200"}},
		{"concurrency = 0", []string{"--concurrency=0"}},
		{"concurrency отрицательный", []string{"--concurrency=-1"}},
		{"max-file-size = 0", []string{"--max-file-size=0"}},
		{"max-file-size отрицательный", []string{"--max-file-size=-1"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseIndexArgs(tc.args)
			if err == nil {
				t.Fatalf("parseIndexArgs(%v) не вернул ошибку", tc.args)
			}
			if _, ok := err.(*UsageError); !ok {
				t.Errorf("ошибка не является *UsageError: %T (%v)", err, err)
			}
		})
	}
}

func TestParseIndexArgsValidBoundary(t *testing.T) {
	// overlap на единицу меньше chunk-size - допустимо.
	opts, err := parseIndexArgs([]string{"--chunk-size=100", "--overlap=99"})
	if err != nil {
		t.Fatalf("неожиданная ошибка: %v", err)
	}
	if opts.ChunkSize != 100 || opts.Overlap != 99 {
		t.Errorf("ChunkSize/Overlap = %d/%d, хотим 100/99", opts.ChunkSize, opts.Overlap)
	}
}

func TestSplitList(t *testing.T) {
	cases := []struct {
		name string
		s    string
		want []string
	}{
		{"пустая строка", "", nil},
		{"только пробелы", "   ", nil},
		{"один элемент", "md", []string{"md"}},
		{"несколько элементов с пробелами", " md , go ,txt", []string{"md", "go", "txt"}},
		{"подряд идущие запятые", "md,,go,", []string{"md", "go"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := splitList(tc.s)
			if !equalStrSlices(got, tc.want) {
				t.Errorf("splitList(%q) = %#v, хотим %#v", tc.s, got, tc.want)
			}
		})
	}
}

func TestNormalizeExts(t *testing.T) {
	cases := []struct {
		name string
		list []string
		want []string
	}{
		{"пустой список", nil, nil},
		{"без точки", []string{"md"}, []string{".md"}},
		{"с точкой", []string{".MD"}, []string{".md"}},
		{"смешанный регистр", []string{"Go", ".TXT"}, []string{".go", ".txt"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeExts(tc.list)
			if !equalStrSlices(got, tc.want) {
				t.Errorf("normalizeExts(%#v) = %#v, хотим %#v", tc.list, got, tc.want)
			}
		})
	}
}

func TestMatchesExt(t *testing.T) {
	cases := []struct {
		name string
		file string
		exts []string
		want bool
	}{
		{"пустой фильтр пропускает всё", "main.go", nil, true},
		{"совпадение точное", "main.go", []string{".go"}, true},
		{"регистронезависимость", "A.MD", []string{".md"}, true},
		{"нет совпадения", "main.go", []string{".md", ".txt"}, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesExt(tc.file, tc.exts)
			if got != tc.want {
				t.Errorf("matchesExt(%q, %#v) = %v, хотим %v", tc.file, tc.exts, got, tc.want)
			}
		})
	}
}

func TestIsNoisyName(t *testing.T) {
	cases := []struct {
		name string
		file string
		want bool
	}{
		{"package-lock.json", "package-lock.json", true},
		{"yarn.lock", "yarn.lock", true},
		{"app.min.js", "app.min.js", true},
		{"style.min.css", "style.min.css", true},
		{"source map", "bundle.js.map", true},
		{"svg", "icon.svg", true},
		{"регистр не важен", "APP.MIN.JS", true},
		{"обычный js", "normal.js", false},
		{"обычный текст", "readme.md", false},
		{"go файл", "main.go", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := isNoisyName(tc.file)
			if got != tc.want {
				t.Errorf("isNoisyName(%q) = %v, хотим %v", tc.file, got, tc.want)
			}
		})
	}
}

func TestAlwaysExcludedDir(t *testing.T) {
	cases := []struct {
		name string
		dir  string
		want bool
	}{
		{".git", ".git", true},
		{"node_modules", "node_modules", true},
		{"vendor", "vendor", true},
		{".senso", ".senso", true},
		{"скрытая директория", ".hidden", true},
		{"обычная директория", "src", false},
		{"текущий каталог", ".", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := alwaysExcludedDir(tc.dir)
			if got != tc.want {
				t.Errorf("alwaysExcludedDir(%q) = %v, хотим %v", tc.dir, got, tc.want)
			}
		})
	}
}

// equalStrSlices сравнивает два срез строк поэлементно.
func equalStrSlices(a, b []string) bool {
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
