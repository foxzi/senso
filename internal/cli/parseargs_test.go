package cli

import (
	"errors"
	"flag"
	"io"
	"reflect"
	"testing"
)

func TestParseArgs(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantArgs []string
		wantK    int
		wantJSON bool
	}{
		{"флаги перед аргументами", []string{"-k", "3", "--json", "a", "b"}, []string{"a", "b"}, 3, true},
		{"флаги после аргументов", []string{"a", "b", "-k", "3", "--json"}, []string{"a", "b"}, 3, true},
		{"флаги между аргументами", []string{"a", "--json", "b", "-k=3"}, []string{"a", "b"}, 3, true},
		{"после -- флаги не разбираются", []string{"-k", "3", "--", "a", "--json"}, []string{"a", "--json"}, 3, false},
		{"-- после аргумента", []string{"a", "--", "-k", "3"}, []string{"a", "-k", "3"}, 0, false},
		{"без аргументов", nil, nil, 0, false},
	}
	for _, c := range cases {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		fs.SetOutput(io.Discard)
		k := fs.Int("k", 0, "")
		asJSON := fs.Bool("json", false, "")

		got, err := parseArgs(fs, c.args)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !reflect.DeepEqual(got, c.wantArgs) || *k != c.wantK || *asJSON != c.wantJSON {
			t.Errorf("%s: args=%v k=%d json=%v, ожидалось args=%v k=%d json=%v",
				c.name, got, *k, *asJSON, c.wantArgs, c.wantK, c.wantJSON)
		}
	}
}

func TestParseArgsUnknownFlagAfterPositional(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	_, err := parseArgs(fs, []string{"a", "--nope"})
	var usageErr *UsageError
	if !errors.As(err, &usageErr) {
		t.Errorf("ошибка = %v, ожидалась UsageError", err)
	}
}

// Флаг после запроса раньше молча становился частью текста запроса.
func TestParseSearchArgsFlagsAfterQuery(t *testing.T) {
	opts, err := parseSearchArgs([]string{"large", "uploads", "--format", "json-v2", "-k", "3"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Query != "large uploads" || opts.Format != "json-v2" || opts.K != 3 {
		t.Errorf("query=%q format=%q k=%d", opts.Query, opts.Format, opts.K)
	}
}

func TestParseShowArgsFlagsAfterRef(t *testing.T) {
	opts, err := parseShowArgs([]string{"/docs/spec.txt#4", "--before", "1"})
	if err != nil {
		t.Fatal(err)
	}
	if opts.Ref != "/docs/spec.txt#4" || opts.Before != 1 {
		t.Errorf("ref=%q before=%d", opts.Ref, opts.Before)
	}
}
