package extract

import (
	"strings"
	"testing"
)

func TestIpynb(t *testing.T) {
	cases := []struct {
		name string
		json string
		want string
	}{
		{
			name: "source строкой",
			json: `{"cells":[{"cell_type":"code","source":"print(1)"}]}`,
			want: "print(1)",
		},
		{
			name: "source массивом строк склеивается без добавления разделителей",
			json: `{"cells":[{"cell_type":"code","source":["a = 1\n","b = 2"]}]}`,
			want: "a = 1\nb = 2",
		},
		{
			name: "ячейки cell_type raw пропускаются",
			json: `{"cells":[{"cell_type":"raw","source":"необработанные данные"},{"cell_type":"code","source":"code"}]}`,
			want: "code",
		},
		{
			name: "пустые ячейки не дают лишних пустых строк",
			json: `{"cells":[{"cell_type":"code","source":"first"},{"cell_type":"code","source":""},{"cell_type":"markdown","source":"   "},{"cell_type":"code","source":"second"}]}`,
			want: "first\n\nsecond",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ipynb([]byte(c.json))
			if err != nil {
				t.Fatalf("ipynb() вернул ошибку: %v", err)
			}
			if got != c.want {
				t.Errorf("ipynb(%q) = %q, хотим %q", c.json, got, c.want)
			}
		})
	}
}

// outputs ячейки (картинки в base64, HTML, трассировки) не должны попадать
// в результат: индексируется только исходный код.
func TestIpynbOutputsIgnored(t *testing.T) {
	const src = `{"cells":[{"cell_type":"code","source":"1+1","outputs":[{"output_type":"display_data","data":{"image/png":"aGVsbG8gYmFzZTY0IGRhdGE="}}]}]}`

	got, err := ipynb([]byte(src))
	if err != nil {
		t.Fatalf("ipynb() вернул ошибку: %v", err)
	}
	if got != "1+1" {
		t.Errorf("ipynb(%q) = %q, хотим %q", src, got, "1+1")
	}
	if strings.Contains(got, "aGVsbG8") {
		t.Errorf("ipynb(%q) = %q, содержит данные outputs, которых там не должно быть", src, got)
	}
}

func TestIpynbErrors(t *testing.T) {
	t.Run("битый JSON даёт ошибку", func(t *testing.T) {
		if _, err := ipynb([]byte(`{"cells":[`)); err == nil {
			t.Fatal("ожидали ошибку при разборе битого JSON")
		}
	})
}
