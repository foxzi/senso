package i18n

import "testing"

// envOf строит getenv поверх карты, чтобы тесты не трогали окружение процесса.
func envOf(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Lang
	}{
		{"пустое окружение - английский", map[string]string{}, EN},
		{"LANG русский", map[string]string{"LANG": "ru_RU.UTF-8"}, RU},
		{"LANG английский", map[string]string{"LANG": "en_US.UTF-8"}, EN},
		{"LANG немецкий - не русский", map[string]string{"LANG": "de_DE.UTF-8"}, EN},
		{"LC_ALL важнее LANG", map[string]string{"LC_ALL": "en_US.UTF-8", "LANG": "ru_RU.UTF-8"}, EN},
		{"LC_MESSAGES важнее LANG", map[string]string{"LC_MESSAGES": "ru_RU.UTF-8", "LANG": "en_US.UTF-8"}, RU},
		{"SENSO_LANG важнее всех", map[string]string{"SENSO_LANG": "en", "LC_ALL": "ru_RU.UTF-8", "LANG": "ru_RU.UTF-8"}, EN},
		{"SENSO_LANG включает русский", map[string]string{"SENSO_LANG": "ru", "LC_ALL": "en_US.UTF-8"}, RU},
		{"локаль C - английский", map[string]string{"LC_ALL": "C"}, EN},
		{"локаль POSIX - английский", map[string]string{"LANG": "POSIX"}, EN},
		{"регистр не важен", map[string]string{"LANG": "RU_RU.UTF-8"}, RU},
		{"пробелы обрезаются", map[string]string{"LANG": "  ru_RU.UTF-8 "}, RU},
		// Пустая переменная не должна маскировать следующую по приоритету:
		// в оболочках LC_ALL часто объявлена, но пуста.
		{"пустая LC_ALL пропускается", map[string]string{"LC_ALL": "", "LANG": "ru_RU.UTF-8"}, RU},
		{"ru как префикс другого языка не ловится", map[string]string{"LANG": "rue_UA"}, RU},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Detect(envOf(c.env)); got != c.want {
				t.Errorf("Detect() = %v, ожидалось %v", got, c.want)
			}
		})
	}
}

func TestTAndTf(t *testing.T) {
	// Язык глобальный, поэтому возвращаем исходное значение после теста.
	saved := Current()
	t.Cleanup(func() { Set(saved) })

	Set(EN)
	if got := T("files", "файлы"); got != "files" {
		t.Errorf("T() при EN = %q, ожидалось %q", got, "files")
	}
	if got := Tf("%d files", "%d файлов", 3); got != "3 files" {
		t.Errorf("Tf() при EN = %q, ожидалось %q", got, "3 files")
	}

	Set(RU)
	if got := T("files", "файлы"); got != "файлы" {
		t.Errorf("T() при RU = %q, ожидалось %q", got, "файлы")
	}
	if got := Tf("%d files", "%d файлов", 3); got != "3 файлов" {
		t.Errorf("Tf() при RU = %q, ожидалось %q", got, "3 файлов")
	}
}

func TestDefaultLangIsEnglish(t *testing.T) {
	// Нулевое значение atomic.Int32 обязано означать английский: если
	// Set почему-то не вызван, вывод должен быть английским, а не русским.
	if EN != 0 {
		t.Fatalf("EN должен быть нулевым значением Lang, получено %d", EN)
	}
}

func TestLangString(t *testing.T) {
	if EN.String() != "en" || RU.String() != "ru" {
		t.Errorf("String() = %q/%q, ожидалось en/ru", EN.String(), RU.String())
	}
}
