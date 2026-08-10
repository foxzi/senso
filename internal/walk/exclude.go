package walk

import (
	"strings"

	"github.com/bmatcuk/doublestar/v4"
)

// hardExcludedDirs - каталоги, которые не обходятся никогда: ни --hidden,
// ни --include-hidden их не открывают. .git содержит историю целиком
// (включая удалённые секреты), .senso - саму базу индекса.
var hardExcludedDirs = []string{".git", ".senso"}

// vendorDirs - каталоги сторонних зависимостей. Их содержимое не является
// кодом проекта и только шумит в результатах поиска.
var vendorDirs = []string{"node_modules", "vendor"}

// alwaysExcludedFiles - шаблоны файлов, которые исключаются всегда,
// даже если они текстовые и проходят по остальным фильтрам.
var alwaysExcludedFiles = []string{
	"*.lock", "*-lock.json", "*.min.js", "*.min.css", "*.map", "*.svg",
}

// secretFilePatterns - файлы, которые почти всегда содержат учётные данные.
// Они исключаются независимо от --hidden: массовое включение скрытых путей
// не должно приводить к попаданию секретов в индекс. Открыть такой файл для
// индексации можно только точечным шаблоном --include-hidden.
var secretFilePatterns = []string{
	".env", ".env.*", "*.env",
	"*.pem", "*.key", "*.p12", "*.pfx", "*.jks", "*.keystore", "*.ppk",
	"id_rsa", "id_dsa", "id_ecdsa", "id_ed25519",
	".netrc", ".pgpass", ".npmrc", ".htpasswd", ".git-credentials",
	"credentials", "credentials.json",
	"secrets.json", "secrets.yaml", "secrets.yml",
}

// isHiddenName сообщает, что имя файла или каталога скрытое, то есть
// начинается с точки. Сам текущий каталог скрытым не считается.
func isHiddenName(name string) bool {
	if name == "." || name == ".." {
		return false
	}
	return strings.HasPrefix(name, ".")
}

// Сравнение имён во всех правилах ниже регистронезависимое: шаблоны
// заданы в нижнем регистре, имя приводится к нему же.

// isHardExcludedDir сообщает, что каталог с таким именем не обходится ни
// при каких флагах.
func isHardExcludedDir(name string) bool {
	return matchesAny(hardExcludedDirs, strings.ToLower(name))
}

// isVendorDir сообщает, что каталог содержит сторонние зависимости.
func isVendorDir(name string) bool {
	return matchesAny(vendorDirs, strings.ToLower(name))
}

// isSecretName сообщает, что имя файла подпадает под безопасные правила
// защиты учётных данных.
func isSecretName(name string) bool {
	return matchesAny(secretFilePatterns, strings.ToLower(name))
}

// isNoisyName сообщает, что файл относится к машинно-генерируемому шуму:
// lock-файлы, минифицированные бандлы, source maps, векторная графика.
func isNoisyName(name string) bool {
	return matchesAny(alwaysExcludedFiles, strings.ToLower(name))
}

// includedByGlob сообщает, что путь явно включён одним из шаблонов
// --include-hidden. Шаблон сравнивается и с путём относительно корня
// обхода, и с именем файла: '.github/**' включает конкретное поддерево,
// а '.env' - одноимённые файлы на любой глубине.
func includedByGlob(patterns []string, rel, name string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, p := range patterns {
		if ok, _ := doublestar.Match(p, rel); ok {
			return true
		}
		if ok, _ := doublestar.Match(p, name); ok {
			return true
		}
	}
	return false
}

// dirMayContainIncluded сообщает, что внутри каталога relDir ещё может
// найтись путь, подходящий под один из шаблонов --include-hidden. Нужно,
// чтобы не отрезать скрытый каталог до того, как обход дойдёт до
// разрешённых файлов внутри него: шаблон '.github/**' сам по себе не
// совпадает с каталогом '.github', но входить в него необходимо.
func dirMayContainIncluded(patterns []string, relDir, name string) bool {
	if includedByGlob(patterns, relDir, name) {
		return true
	}
	for _, p := range patterns {
		if prefixMayMatch(p, relDir) {
			return true
		}
	}
	return false
}

// prefixMayMatch сравнивает шаблон с каталогом посегментно и сообщает,
// что шаблон ещё может совпасть с чем-то внутри этого каталога.
func prefixMayMatch(pattern, relDir string) bool {
	if relDir == "" || relDir == "." {
		return true
	}
	patSegs := strings.Split(pattern, "/")
	dirSegs := strings.Split(relDir, "/")

	for i, seg := range dirSegs {
		if i >= len(patSegs) {
			// Путь глубже шаблона: продолжение возможно только если
			// шаблон заканчивался на '**'.
			return patSegs[len(patSegs)-1] == "**"
		}
		if patSegs[i] == "**" {
			return true
		}
		if ok, _ := doublestar.Match(patSegs[i], seg); !ok {
			return false
		}
	}
	// Шаблон длиннее пути и совпал по всем его сегментам - внутри ещё
	// может найтись подходящий путь.
	return true
}
