// Package embed отвечает за получение векторных эмбеддингов текста
// через локальный сервер Ollama.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"time"

	"senso/internal/i18n"
)

// maxAttempts - число попыток запроса (1 основная + 2 повторных).
const maxAttempts = 3

// Client - HTTP-клиент к Ollama для получения эмбеддингов.
type Client struct {
	baseURL string
	model   string
	http    *http.Client

	// retryBase - базовая задержка перед повтором; удваивается на каждой
	// следующей попытке. Неэкспортируемое поле, чтобы тесты могли ускорить
	// повторные попытки без реального ожидания секунд.
	retryBase time.Duration
}

// New создаёт клиент Ollama с адресом сервера baseURL и именем модели model.
func New(baseURL, model string) *Client {
	return &Client{
		baseURL:   baseURL,
		model:     model,
		http:      &http.Client{Timeout: 120 * time.Second},
		retryBase: time.Second,
	}
}

// embedRequest - тело запроса к /api/embed.
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

// embedResponse - тело ответа от /api/embed.
type embedResponse struct {
	Model      string      `json:"model"`
	Embeddings [][]float32 `json:"embeddings"`
}

// Embed получает эмбеддинги для набора текстов. Порядок векторов в ответе
// соответствует порядку texts. Векторы не нормализуются - это должен
// сделать вызывающий код через Normalize.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}

	reqBody, err := json.Marshal(embedRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, fmt.Errorf(i18n.T("ollama unavailable at %s (model %s): %w", "ollama недоступен по адресу %s (модель %s): %w"), c.baseURL, c.model, err)
	}

	var lastErr error
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			delay := c.retryBase * time.Duration(1<<(attempt-1))
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		embeddings, retry, err := c.doRequest(ctx, reqBody)
		if err == nil {
			if len(embeddings) != len(texts) {
				return nil, fmt.Errorf(
					i18n.T(
						"ollama unavailable at %s (model %s): got %d vectors, expected %d",
						"ollama недоступен по адресу %s (модель %s): получено %d векторов, ожидалось %d",
					),
					c.baseURL, c.model, len(embeddings), len(texts),
				)
			}
			return embeddings, nil
		}

		lastErr = err
		if !retry {
			return nil, err
		}
	}

	return nil, lastErr
}

// doRequest выполняет один HTTP-запрос к Ollama. Возвращает retry=true,
// если ошибку стоит повторить (сетевая ошибка или HTTP 5xx).
func (c *Client) doRequest(ctx context.Context, reqBody []byte) ([][]float32, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(reqBody))
	if err != nil {
		return nil, false, fmt.Errorf(i18n.T("ollama unavailable at %s (model %s): %w", "ollama недоступен по адресу %s (модель %s): %w"), c.baseURL, c.model, err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, true, fmt.Errorf(i18n.T("ollama unavailable at %s (model %s): %w", "ollama недоступен по адресу %s (модель %s): %w"), c.baseURL, c.model, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, true, fmt.Errorf(i18n.T("ollama unavailable at %s (model %s): %w", "ollama недоступен по адресу %s (модель %s): %w"), c.baseURL, c.model, err)
	}

	if resp.StatusCode >= 500 {
		return nil, true, fmt.Errorf(
			i18n.T(
				"ollama unavailable at %s (model %s): server error %d: %s",
				"ollama недоступен по адресу %s (модель %s): ошибка сервера %d: %s",
			),
			c.baseURL, c.model, resp.StatusCode, string(body),
		)
	}
	if resp.StatusCode >= 400 {
		return nil, false, fmt.Errorf(
			i18n.T(
				"ollama unavailable at %s (model %s): request error %d: %s",
				"ollama недоступен по адресу %s (модель %s): ошибка запроса %d: %s",
			),
			c.baseURL, c.model, resp.StatusCode, string(body),
		)
	}

	var parsed embedResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return nil, false, fmt.Errorf(i18n.T("ollama unavailable at %s (model %s): %w", "ollama недоступен по адресу %s (модель %s): %w"), c.baseURL, c.model, err)
	}

	return parsed.Embeddings, false, nil
}

// Normalize выполняет L2-нормализацию вектора на месте. Если норма близка
// к нулю (<1e-12), вектор оставляется без изменений, чтобы не делить на ноль.
func Normalize(v []float32) {
	var sumSquares float64
	for _, x := range v {
		sumSquares += float64(x) * float64(x)
	}
	norm := math.Sqrt(sumSquares)
	if norm < 1e-12 {
		return
	}
	for i := range v {
		v[i] = float32(float64(v[i]) / norm)
	}
}
