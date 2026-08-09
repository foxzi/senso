// Package version хранит версию сборки senso.
//
// Значения проставляются линковщиком через -ldflags -X при сборке (см.
// Makefile, который берёт их из git describe). Если сборка шла без ldflags -
// например, через "go install senso@latest" или "go build" вручную, - версия
// восстанавливается из информации, которую Go встраивает в бинарник сам.
package version

import (
	"runtime/debug"
	"strings"
)

// Значения по умолчанию перекрываются линковщиком:
//
//	go build -ldflags "-X senso/internal/version.version=v1.2.3"
var (
	version = ""
	commit  = ""
	date    = ""
)

// devVersion - версия сборки, о которой ничего не известно: ни ldflags,
// ни данных git в бинарнике.
const devVersion = "dev"

// Version возвращает версию сборки: значение из ldflags, иначе версию модуля
// из встроенной информации о сборке, иначе "dev".
func Version() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		// При "go install module@version" сюда попадает сам тег; при сборке
		// из рабочего дерева Go пишет "(devel)", что версией не является.
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return devVersion
}

// Commit возвращает хеш коммита сборки: значение из ldflags, иначе
// vcs.revision из встроенной информации о сборке. Пустая строка означает,
// что коммит неизвестен.
func Commit() string {
	if commit != "" {
		return commit
	}
	return buildSetting("vcs.revision")
}

// Date возвращает дату сборки: значение из ldflags, иначе vcs.time из
// встроенной информации о сборке. Пустая строка означает, что дата
// неизвестна.
func Date() string {
	if date != "" {
		return date
	}
	return buildSetting("vcs.time")
}

// String собирает строку версии для вывода команды version. Коммит и дата
// добавляются в скобках, только если известны.
func String() string {
	var b strings.Builder
	b.WriteString("senso ")
	b.WriteString(Version())

	var extra []string
	if c := Commit(); c != "" {
		extra = append(extra, shortCommit(c))
	}
	if d := Date(); d != "" {
		extra = append(extra, d)
	}
	if len(extra) > 0 {
		b.WriteString(" (")
		b.WriteString(strings.Join(extra, ", "))
		b.WriteString(")")
	}
	return b.String()
}

// shortCommit укорачивает полный хеш коммита до семи символов - привычной
// в git короткой формы. Хеш короче семи символов возвращается как есть.
func shortCommit(c string) string {
	const shortLen = 7
	if len(c) <= shortLen {
		return c
	}
	return c[:shortLen]
}

// buildSetting возвращает значение настройки сборки key из встроенной
// информации о сборке или пустую строку, если её там нет.
func buildSetting(key string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range info.Settings {
		if s.Key == key {
			return s.Value
		}
	}
	return ""
}
