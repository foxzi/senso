package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCheckReportAddFileKeepsCountersInSync(t *testing.T) {
	rep := newCheckReport()
	rep.addFile("/a.txt", statusChanged, "")
	rep.addFile("/b.txt", statusMissing, "")
	rep.addFile("/c.txt", statusUnindexed, "")
	rep.addFile("/d.txt", statusExcluded, "gitignore")

	if rep.Changed != 1 || rep.Missing != 1 || rep.Unindexed != 1 || rep.Excluded != 1 {
		t.Fatalf("счётчики не совпали: %+v", rep)
	}
	if len(rep.Files) != 4 {
		t.Fatalf("в списке %d файлов, ожидалось 4", len(rep.Files))
	}
	if rep.ExcludedByReason["gitignore"] != 1 {
		t.Errorf("причина исключения не учтена: %v", rep.ExcludedByReason)
	}
	if rep.Files[3].Reason != "gitignore" {
		t.Errorf("reason = %q, ожидалось gitignore", rep.Files[3].Reason)
	}
}

// Порядок списка не должен зависеть от порядка обхода каталогов, а внутри
// статуса - от файловой системы.
func TestLimitFilesSortsByStatusThenPath(t *testing.T) {
	rep := newCheckReport()
	rep.addFile("/z.txt", statusUnindexed, "")
	rep.addFile("/b.txt", statusChanged, "")
	rep.addFile("/a.txt", statusExcluded, "hidden")
	rep.addFile("/a.txt", statusChanged, "")
	rep.addFile("/y.txt", statusMissing, "")

	limitFiles(rep, 0)

	var got []string
	for _, f := range rep.Files {
		got = append(got, f.Status+":"+f.Path)
	}
	want := []string{
		"changed:/a.txt",
		"changed:/b.txt",
		"missing:/y.txt",
		"excluded:/a.txt",
		"unindexed:/z.txt",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("порядок = %v, ожидался %v", got, want)
		}
	}
	if rep.FilesTruncated {
		t.Error("список не обрезался, files_truncated должен быть false")
	}
}

// Обрезка не должна выбрасывать изменившиеся файлы ради непроиндексированных:
// их обычно на порядок больше.
func TestLimitFilesKeepsActionableStatusesFirst(t *testing.T) {
	rep := newCheckReport()
	for i := 0; i < 10; i++ {
		rep.addFile("/new"+string(rune('0'+i))+".txt", statusUnindexed, "")
	}
	rep.addFile("/changed.txt", statusChanged, "")

	limitFiles(rep, 2)

	if len(rep.Files) != 2 {
		t.Fatalf("в списке %d файлов, ожидалось 2", len(rep.Files))
	}
	if rep.Files[0].Status != statusChanged {
		t.Errorf("первым должен идти changed, получено %q", rep.Files[0].Status)
	}
	if !rep.FilesTruncated {
		t.Error("files_truncated должен быть true")
	}
	if rep.Unindexed != 10 || rep.Changed != 1 {
		t.Errorf("счётчики должны остаться полными: %+v", rep)
	}
}

func TestParseCheckArgsNegativeListLimit(t *testing.T) {
	if _, err := parseCheckArgs([]string{"--list-limit", "-1"}); err == nil {
		t.Fatal("отрицательный --list-limit должен быть ошибкой использования")
	}
}

func TestParseCheckArgsListLimitDefault(t *testing.T) {
	opts, err := parseCheckArgs(nil)
	if err != nil {
		t.Fatal(err)
	}
	if opts.ListLimit != defaultFileListLimit {
		t.Errorf("ListLimit = %d, ожидалось %d", opts.ListLimit, defaultFileListLimit)
	}
}

// Сквозная проверка: после правки дерева JSON должен показывать не только
// счётчики, но и конкретные пути с их статусами.
func TestRunCheckJSONListsDivergingFiles(t *testing.T) {
	dir := t.TempDir()
	changed := filepath.Join(dir, "changed.txt")
	gone := filepath.Join(dir, "gone.txt")
	for _, p := range []string{changed, gone} {
		if err := os.WriteFile(p, []byte("hello world"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	prev, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(prev)

	if err := RunIndex([]string{"--quiet", "."}); err != nil {
		t.Fatalf("RunIndex: %v", err)
	}

	if err := os.WriteFile(changed, []byte("hello other world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fresh.txt"), []byte("brand new"), 0o644); err != nil {
		t.Fatal(err)
	}

	out := withCapturedStdout(t, func() {
		if code := runCheckIn(t, dir, "--json", "."); code != exitStale {
			t.Errorf("код выхода = %d, ожидался %d", code, exitStale)
		}
	})

	var rep checkReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("разбор JSON %q: %v", out, err)
	}

	got := make(map[string]string, len(rep.Files))
	for _, f := range rep.Files {
		got[filepath.Base(f.Path)] = f.Status
	}
	want := map[string]string{
		"changed.txt": statusChanged,
		"gone.txt":    statusMissing,
		"fresh.txt":   statusUnindexed,
	}
	for name, status := range want {
		if got[name] != status {
			t.Errorf("%s: статус %q, ожидался %q (files=%v)", name, got[name], status, rep.Files)
		}
	}
	if !strings.Contains(out, `"files"`) {
		t.Error("в JSON нет поля files")
	}
}
