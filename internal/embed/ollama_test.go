package embed

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEmbedSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatal(err)
		}
		resp := embedResponse{
			Model:      req.Model,
			Embeddings: make([][]float32, len(req.Input)),
		}
		for i := range req.Input {
			resp.Embeddings[i] = []float32{1, 2, 3}
		}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL, "bge-m3")
	vecs, err := c.Embed(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 2 {
		t.Fatalf("получено %d векторов, ожидалось 2", len(vecs))
	}
}

func TestEmbedEmptyInput(t *testing.T) {
	c := New("http://unused", "bge-m3")
	vecs, err := c.Embed(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if vecs != nil {
		t.Errorf("Embed(nil) = %v, ожидалось nil", vecs)
	}
}

func TestEmbedRetryOn500(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte("internal error"))
			return
		}
		resp := embedResponse{Embeddings: [][]float32{{1, 2}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL, "bge-m3")
	c.retryBase = time.Millisecond

	vecs, err := c.Embed(context.Background(), []string{"a"})
	if err != nil {
		t.Fatal(err)
	}
	if len(vecs) != 1 {
		t.Fatalf("получено %d векторов, ожидалось 1", len(vecs))
	}
	if calls != 2 {
		t.Errorf("сделано %d попыток, ожидалось 2", calls)
	}
}

func TestEmbedNoRetryOn400(t *testing.T) {
	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("bad request"))
	}))
	defer srv.Close()

	c := New(srv.URL, "bge-m3")
	c.retryBase = time.Millisecond

	_, err := c.Embed(context.Background(), []string{"a"})
	if err == nil {
		t.Fatal("ожидалась ошибка при 400")
	}
	if calls != 1 {
		t.Errorf("сделано %d попыток, ожидалась ровно 1 (без повторов на 4xx)", calls)
	}
}

func TestEmbedCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := embedResponse{Embeddings: [][]float32{{1, 2}}}
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	c := New(srv.URL, "bge-m3")
	c.retryBase = time.Millisecond

	_, err := c.Embed(context.Background(), []string{"a", "b"})
	if err == nil {
		t.Fatal("ожидалась ошибка при несовпадении числа векторов")
	}
}

func TestNormalize(t *testing.T) {
	v := []float32{3, 4}
	Normalize(v)

	var sumSquares float64
	for _, x := range v {
		sumSquares += float64(x) * float64(x)
	}
	norm := math.Sqrt(sumSquares)
	if math.Abs(norm-1) > 1e-6 {
		t.Errorf("норма после Normalize = %v, ожидалось 1", norm)
	}
}

func TestNormalizeZeroVector(t *testing.T) {
	v := []float32{0, 0, 0}
	Normalize(v)
	for _, x := range v {
		if x != 0 {
			t.Errorf("нулевой вектор изменился после Normalize: %v", v)
		}
	}
}
