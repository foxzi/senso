package cli

import "testing"

func TestBatchStringsEmpty(t *testing.T) {
	if got := batchStrings(nil, 32); got != nil {
		t.Errorf("batchStrings(nil) = %#v, хотим nil", got)
	}
}

func TestBatchStringsExactMultiple(t *testing.T) {
	items := []string{"a", "b", "c", "d"}
	got := batchStrings(items, 2)
	want := [][]string{{"a", "b"}, {"c", "d"}}
	if len(got) != len(want) {
		t.Fatalf("batchStrings вернул %d батчей, хотим %d", len(got), len(want))
	}
	for i := range want {
		if len(got[i]) != len(want[i]) {
			t.Fatalf("батч %d: длина %d, хотим %d", i, len(got[i]), len(want[i]))
		}
		for j := range want[i] {
			if got[i][j] != want[i][j] {
				t.Errorf("батч %d[%d] = %q, хотим %q", i, j, got[i][j], want[i][j])
			}
		}
	}
}

func TestBatchStringsRemainder(t *testing.T) {
	items := []string{"a", "b", "c"}
	got := batchStrings(items, 2)
	if len(got) != 2 {
		t.Fatalf("batchStrings вернул %d батчей, хотим 2", len(got))
	}
	if len(got[0]) != 2 || len(got[1]) != 1 {
		t.Fatalf("размеры батчей = %d, %d; хотим 2, 1", len(got[0]), len(got[1]))
	}
	if got[1][0] != "c" {
		t.Errorf("последний батч = %#v, хотим [c]", got[1])
	}
}

func TestBatchStringsSmallerThanSize(t *testing.T) {
	items := []string{"a"}
	got := batchStrings(items, 32)
	if len(got) != 1 || len(got[0]) != 1 {
		t.Fatalf("batchStrings(%v, 32) = %#v", items, got)
	}
}

func TestParseIndexArgsEmbedDefaultFalse(t *testing.T) {
	opts, err := parseIndexArgs(nil)
	if err != nil {
		t.Fatalf("parseIndexArgs(nil) вернул ошибку: %v", err)
	}
	if opts.Embed != false {
		t.Errorf("Embed = %v, хотим false", opts.Embed)
	}
}

func TestParseIndexArgsEmbedFlag(t *testing.T) {
	opts, err := parseIndexArgs([]string{"--embed"})
	if err != nil {
		t.Fatalf("parseIndexArgs(--embed) вернул ошибку: %v", err)
	}
	if opts.Embed != true {
		t.Errorf("Embed = %v, хотим true", opts.Embed)
	}
}

func TestApplyBackfill(t *testing.T) {
	cases := []struct {
		name     string
		action   fileAction
		backfill bool
		want     fileAction
	}{
		{"skip без backfill остаётся skip", actionSkip, false, actionSkip},
		{"skip с backfill становится reindex", actionSkip, true, actionReindex},
		{"touch с backfill не меняется", actionTouch, true, actionTouch},
		{"reindex с backfill не меняется", actionReindex, true, actionReindex},
		{"touch без backfill не меняется", actionTouch, false, actionTouch},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := applyBackfill(tc.action, tc.backfill); got != tc.want {
				t.Errorf("applyBackfill(%v, %v) = %v, хотим %v", tc.action, tc.backfill, got, tc.want)
			}
		})
	}
}
