package extract

import (
	"encoding/json"
	"strings"
)

// notebook - блокнот Jupyter в формате nbformat 4.
//
// Читается только исходный код ячеек. Результаты выполнения (outputs)
// намеренно игнорируются: там лежат картинки в base64, HTML-таблицы и
// трассировки ошибок, которые засоряют индекс и не несут смысла при поиске
// по содержимому блокнота.
type notebook struct {
	Cells []struct {
		Type   string          `json:"cell_type"`
		Source json.RawMessage `json:"source"`
	} `json:"cells"`
}

// ipynb извлекает текст ячеек блокнота Jupyter (.ipynb).
func ipynb(data []byte) (string, error) {
	var nb notebook
	if err := json.Unmarshal(data, &nb); err != nil {
		return "", err
	}

	var b strings.Builder
	for _, c := range nb.Cells {
		if c.Type == "raw" {
			continue
		}
		src := jsonText(c.Source)
		if strings.TrimSpace(src) == "" {
			continue
		}
		b.WriteString(strings.TrimRight(src, "\n"))
		b.WriteString("\n\n")
	}

	return strings.TrimSpace(b.String()), nil
}

// jsonText приводит поле блокнота к строке. В nbformat одно и то же поле
// записывается либо строкой целиком, либо массивом строк с сохранёнными
// переводами строк на концах; встречаются оба варианта.
func jsonText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var lines []string
	if err := json.Unmarshal(raw, &lines); err == nil {
		return strings.Join(lines, "")
	}
	return ""
}
