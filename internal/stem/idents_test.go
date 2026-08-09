package stem

import "testing"

func TestIdents(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "camelCase раскладывается на слова и слитную форму",
			in:   "ReplaceFile",
			want: "replac file replacefil",
		},
		{
			name: "snake_case даёт те же слова, что и camelCase",
			in:   "replace_file",
			want: "replac file replacefil",
		},
		{
			name: "kebab-case раскладывается так же",
			in:   "replace-file",
			want: "replac file replacefil",
		},
		{
			name: "аббревиатура отделяется от следующего слова",
			in:   "HTTPServer",
			want: "http server httpserver",
		},
		{
			name: "цифры остаются частью своего слова",
			in:   "utf8Decode",
			want: "utf8 decod utf8decod",
		},
		{
			name: "простое слово пропускается, оно уже есть в тексте",
			in:   "файл",
			want: "",
		},
		{
			name: "идентификаторы находятся внутри обычного текста",
			in:   "func ReplaceFile(path string) error",
			want: "replac file replacefil",
		},
		{
			name: "несколько идентификаторов идут в порядке появления",
			in:   "SearchLexical -> fts_chunks",
			want: "search lexic searchlex fts chunk ftschunk",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := Idents(c.in); got != c.want {
				t.Errorf("Idents(%q) = %q, хотим %q", c.in, got, c.want)
			}
		})
	}
}

func TestIdentsMatchesQuery(t *testing.T) {
	// Главное свойство: как ни напиши идентификатор в запросе - по частям,
	// слитно или в другом стиле, - запрос попадает в то, что лежит в ids.
	indexed := Idents("func ReplaceFile(path string)")

	queries := []string{"ReplaceFile", "replace_file", "replace file", "files"}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			for _, token := range Tokens(Query(q)) {
				if !hasToken(indexed, token) {
					t.Errorf("токен %q запроса %q не найден в ids %q", token, q, indexed)
				}
			}
		})
	}
}

// hasToken сообщает, встречается ли токен в строке индекса как отдельное слово.
func hasToken(indexed, token string) bool {
	for _, t := range Tokens(indexed) {
		if t == token {
			return true
		}
	}
	return false
}

func TestPath(t *testing.T) {
	cases := []struct {
		name  string
		path  string
		wants []string
	}{
		{
			name:  "сегменты пути, имя и расширение становятся токенами",
			path:  "/home/user/internal/store/store.go",
			wants: []string{"intern", "store", "go"},
		},
		{
			name:  "составное имя файла ищется и по частям, и целиком",
			path:  "/src/user_repository.go",
			wants: []string{"user", "repositori", "userrepositori"},
		},
		{
			name:  "camelCase в имени файла тоже раскладывается",
			path:  "/src/UserRepository.java",
			wants: []string{"user", "repositori", "java"},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Path(c.path)
			for _, w := range c.wants {
				if !hasToken(got, w) {
					t.Errorf("Path(%q) = %q, не хватает токена %q", c.path, got, w)
				}
			}
		})
	}
}
