package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// binaryContent - содержимое, которое index пропускает как бинарное.
var binaryContent = append([]byte("PNG\x00\x00"), bytes.Repeat([]byte{0xff, 0x00}, 64)...)

// Сквозной сценарий бага: index пропускает бинарный файл по содержимому,
// значит check не должен считать его непроиндексированным, иначе цикл
// "check || index" зациклится - переиндексация бинарник в базу не добавит.
func TestRunCheckFreshWithBinaryFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), binaryContent, 0o644); err != nil {
		t.Fatal(err)
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

	out := withCapturedStdout(t, func() {
		if code := runCheckIn(t, dir, "--json", "."); code != 0 {
			t.Errorf("exit code = %d, want 0", code)
		}
	})

	var rep checkReport
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("некорректный JSON: %v\n%s", err, out)
	}
	if !rep.Fresh {
		t.Errorf("fresh = false, индекс с пропущенным бинарником должен быть свежим: %+v", rep)
	}
	if rep.Unindexed != 0 {
		t.Errorf("unindexed = %d, want 0", rep.Unindexed)
	}
	if rep.Skipped != 1 {
		t.Errorf("skipped = %d, want 1", rep.Skipped)
	}

	found := false
	for _, f := range rep.Files {
		if f.Status == statusSkipped {
			found = true
			if f.Reason != skipBinary {
				t.Errorf("reason = %q, want %q", f.Reason, skipBinary)
			}
		}
	}
	if !found {
		t.Error("в списке files нет записи со статусом skipped")
	}
}

// Обычный текстовый файл, которого нет в индексе, по-прежнему должен
// считаться непроиндексированным и делать индекс устаревшим.
func TestRunCheckNewTextFileStillStale(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("hello world"), 0o644); err != nil {
		t.Fatal(err)
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
	if err := os.WriteFile(filepath.Join(dir, "b.txt"), []byte("new text"), 0o644); err != nil {
		t.Fatal(err)
	}

	if code := runCheckIn(t, dir, "--quiet"); code != exitStale {
		t.Errorf("exit code = %d, want %d", code, exitStale)
	}
}

func TestSkipReason(t *testing.T) {
	dir := t.TempDir()
	opts := indexOptions{MaxFileSize: 10}

	write := func(name string, data []byte) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	cases := []struct {
		name string
		path string
		want string
	}{
		{"бинарный", write("bin.dat", binaryContent), skipBinary},
		{"пустой", write("empty.txt", nil), skipEmpty},
		{"текстовый", write("ok.txt", []byte("plain text")), ""},
	}
	for _, tc := range cases {
		got, err := skipReason(tc.path, opts)
		if err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s: skipReason = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestSkipReasonTooLarge(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.txt")
	// MaxFileSize задаётся в мегабайтах, поэтому лимит для теста - 1 МБ.
	if err := os.WriteFile(p, bytes.Repeat([]byte("a"), 1024*1024+1), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := skipReason(p, indexOptions{MaxFileSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	if got != skipTooLarge {
		t.Errorf("skipReason = %q, want %q", got, skipTooLarge)
	}
}
