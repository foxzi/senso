package stem

import (
	"strings"
	"testing"
)

func TestFold(t *testing.T) {
	cases := map[string]string{
		"Ёлка":    "елка",
		"ЁЖИК":    "ежик",
		"Привет":  "привет",
		"HELLO":   "hello",
		"ёлка ёж": "елка еж",
	}
	for in, want := range cases {
		if got := Fold(in); got != want {
			t.Errorf("Fold(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTokens(t *testing.T) {
	got := Tokens("поиск, файлов! и/или 2024-файл")
	want := []string{"поиск", "файлов", "и", "или", "2024", "файл"}
	if len(got) != len(want) {
		t.Fatalf("Tokens() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Tokens()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestTextContainsStems(t *testing.T) {
	got := Text("Локальный поиск по файлам")
	if !strings.Contains(got, "файл") {
		t.Errorf("Text() = %q, must contain %q", got, "файл")
	}
	if !strings.Contains(got, "поиск") {
		t.Errorf("Text() = %q, must contain %q", got, "поиск")
	}
}

func TestTextPreservesTokenCount(t *testing.T) {
	got := Text("Локальный поиск по файлам")
	tokens := strings.Split(got, " ")
	if len(tokens) != 4 {
		t.Fatalf("Text() = %q, got %d tokens, want 4", got, len(tokens))
	}
}

func TestStemRussianWordForms(t *testing.T) {
	forms := []string{"файлами", "файлов", "файл"}
	stems := make([]string, len(forms))
	for i, f := range forms {
		stems[i] = stemToken(f)
	}
	for i := 1; i < len(stems); i++ {
		if stems[i] != stems[0] {
			t.Errorf("stemToken(%q) = %q, stemToken(%q) = %q, want equal", forms[i], stems[i], forms[0], stems[0])
		}
	}
}

func TestStemRussianIndexation(t *testing.T) {
	a := stemToken("индексация")
	b := stemToken("индексации")
	if a != b {
		t.Errorf("stemToken(индексация) = %q, stemToken(индексации) = %q, want equal", a, b)
	}
}

func TestStemEnglish(t *testing.T) {
	a := stemToken("searching")
	b := stemToken("search")
	if a != b {
		t.Errorf("stemToken(searching) = %q, stemToken(search) = %q, want equal", a, b)
	}
}

func TestQuery(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"поиск файлов", `"поиск" "файл"`},
		{`"поиск файлов"`, `"поиск файл"`},
		{"поис*", `"поис"*`},
		{"(файлам)", `"файл"`},
		{"", ""},
		{"!!!", ""},
	}
	for _, c := range cases {
		if got := Query(c.in); got != c.want {
			t.Errorf("Query(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestQueryUnclosedQuoteDoesNotPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("Query panicked on unclosed quote: %v", r)
		}
	}()
	got := Query(`поиск "файлов`)
	want := `"поиск" "файл"`
	if got != want {
		t.Errorf("Query(unclosed) = %q, want %q", got, want)
	}
}
