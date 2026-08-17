package walk

import "testing"

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
			if got := isNoisyName(tc.file, nil); got != tc.want {
				t.Errorf("isNoisyName(%q) = %v, хотим %v", tc.file, got, tc.want)
			}
		})
	}
}

func TestIsNoisyNameCustomPatterns(t *testing.T) {
	patterns := []string{"*.generated.go", "*.pb.go"}

	cases := []struct {
		name string
		file string
		want bool
	}{
		{"свой шаблон совпал", "api.pb.go", true},
		{"свой шаблон совпал, другой регистр", "API.Generated.GO", true},
		{"встроенный шаблон больше не действует", "yarn.lock", false},
		{"встроенный шаблон больше не действует, svg", "icon.svg", false},
		{"обычный файл", "main.go", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNoisyName(tc.file, patterns); got != tc.want {
				t.Errorf("isNoisyName(%q, %v) = %v, хотим %v", tc.file, patterns, got, tc.want)
			}
		})
	}
}

func TestIsHardExcludedDirAndVendorDir(t *testing.T) {
	cases := []struct {
		name       string
		dir        string
		wantHard   bool
		wantVendor bool
	}{
		{".git", ".git", true, false},
		{".senso", ".senso", true, false},
		{"node_modules", "node_modules", false, true},
		{"vendor", "vendor", false, true},
		{"скрытая директория не жёсткая", ".hidden", false, false},
		{"обычная директория", "src", false, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isHardExcludedDir(tc.dir); got != tc.wantHard {
				t.Errorf("isHardExcludedDir(%q) = %v, хотим %v", tc.dir, got, tc.wantHard)
			}
			if got := isVendorDir(tc.dir); got != tc.wantVendor {
				t.Errorf("isVendorDir(%q) = %v, хотим %v", tc.dir, got, tc.wantVendor)
			}
		})
	}
}

func TestIsHiddenName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{".env", true},
		{".github", true},
		{".", false},
		{"..", false},
		{"src", false},
		{"main.go", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isHiddenName(tc.name); got != tc.want {
				t.Errorf("isHiddenName(%q) = %v, хотим %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIsSecretName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{".env", true},
		{".env.local", true},
		{"production.env", true},
		{"server.pem", true},
		{"tls.key", true},
		{"id_rsa", true},
		{"id_ed25519", true},
		{".npmrc", true},
		{".netrc", true},
		{".git-credentials", true},
		{"credentials.json", true},
		{"secrets.yaml", true},
		{"ID_RSA", true},
		{"environment.go", false},
		{"keyboard.md", false},
		{"main.go", false},
		{"secrets.md", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isSecretName(tc.name); got != tc.want {
				t.Errorf("isSecretName(%q) = %v, хотим %v", tc.name, got, tc.want)
			}
		})
	}
}

func TestIncludedByGlob(t *testing.T) {
	patterns := []string{".github/**", ".env"}

	cases := []struct {
		rel  string
		name string
		want bool
	}{
		{".github/workflows/ci.yml", "ci.yml", true},
		{".env", ".env", true},
		{"config/.env", ".env", true}, // шаблон сравнивается и с именем
		{".agents/notes.md", "notes.md", false},
		{"main.go", "main.go", false},
	}

	for _, tc := range cases {
		t.Run(tc.rel, func(t *testing.T) {
			if got := includedByGlob(patterns, tc.rel, tc.name); got != tc.want {
				t.Errorf("includedByGlob(%q, %q) = %v, хотим %v", tc.rel, tc.name, got, tc.want)
			}
		})
	}
}

func TestIncludedByGlobEmptyPatterns(t *testing.T) {
	if includedByGlob(nil, ".env", ".env") {
		t.Error("пустой список шаблонов не должен ничего включать")
	}
}

func TestDirMayContainIncluded(t *testing.T) {
	cases := []struct {
		name     string
		patterns []string
		relDir   string
		dirName  string
		want     bool
	}{
		{"поддерево шаблона", []string{".github/**"}, ".github", ".github", true},
		{"вложенный каталог поддерева", []string{".github/**"}, ".github/wf", "wf", true},
		{"чужой каталог", []string{".github/**"}, ".agents", ".agents", false},
		{"точное имя каталога", []string{".agents"}, ".agents", ".agents", true},
		{"глубже шаблона без **", []string{".agents"}, ".agents/.cache", ".cache", false},
		{"файловый шаблон в корне", []string{".env"}, ".config", ".config", false},
		{"шаблон глубже пути", []string{".config/sub/**"}, ".config", ".config", true},
		{"звёздочка в сегменте", []string{".*/docs/**"}, ".foo", ".foo", true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := dirMayContainIncluded(tc.patterns, tc.relDir, tc.dirName)
			if got != tc.want {
				t.Errorf("dirMayContainIncluded(%#v, %q) = %v, хотим %v", tc.patterns, tc.relDir, got, tc.want)
			}
		})
	}
}
