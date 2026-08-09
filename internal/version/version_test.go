package version

import (
	"strings"
	"testing"
)

// withLDFlags временно подменяет значения, которые в реальной сборке
// проставляет линковщик, и возвращает их после теста.
func withLDFlags(t *testing.T, v, c, d string) {
	t.Helper()
	oldV, oldC, oldD := version, commit, date
	version, commit, date = v, c, d
	t.Cleanup(func() { version, commit, date = oldV, oldC, oldD })
}

// TestVersionFromLDFlags проверяет, что значение из ldflags имеет приоритет.
func TestVersionFromLDFlags(t *testing.T) {
	withLDFlags(t, "v1.2.3", "abcdef1234567", "2026-01-02T03:04:05Z")

	if got := Version(); got != "v1.2.3" {
		t.Errorf("Version() = %q, хотим v1.2.3", got)
	}
	if got := String(); got != "senso v1.2.3 (abcdef1, 2026-01-02T03:04:05Z)" {
		t.Errorf("String() = %q", got)
	}
}

// TestStringWithoutCommitAndDate проверяет, что при неизвестных коммите и
// дате скобки не печатаются. Пустые ldflags заставляют функции обратиться к
// встроенной информации о сборке, поэтому проверяем формат, а не константу.
func TestStringWithoutCommitAndDate(t *testing.T) {
	withLDFlags(t, "v1.2.3", "", "")

	got := String()
	if !strings.HasPrefix(got, "senso v1.2.3") {
		t.Fatalf("String() = %q, ожидался префикс senso v1.2.3", got)
	}
	if strings.Contains(got, "()") {
		t.Errorf("String() = %q, пустые скобки печатать не нужно", got)
	}
}

// TestVersionFallsBackToDev проверяет, что без ldflags и без данных о сборке
// версия не оказывается пустой строкой.
func TestVersionFallsBackToDev(t *testing.T) {
	withLDFlags(t, "", "", "")

	if got := Version(); got == "" {
		t.Error("Version() вернул пустую строку")
	}
}

// TestShortCommit проверяет укорачивание хеша до семи символов.
func TestShortCommit(t *testing.T) {
	cases := map[string]string{
		"abcdef1234567890": "abcdef1",
		"abcdef1":          "abcdef1",
		"abc":              "abc",
		"":                 "",
	}
	for in, want := range cases {
		if got := shortCommit(in); got != want {
			t.Errorf("shortCommit(%q) = %q, хотим %q", in, got, want)
		}
	}
}
