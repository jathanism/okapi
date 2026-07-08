package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
)

// newFakeServer returns an httptest server that implements just enough of the
// items API to round-trip every operation in openapi.yaml. It is intentionally
// canned: every GET returns the same item, every list returns the same list.
// The point is to exercise the wire contract, not to model real persistence.
func newFakeServer() *httptest.Server {
	mux := http.NewServeMux()

	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})

	mux.HandleFunc("/items", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, http.StatusOK, map[string]any{
				"items": []map[string]any{sampleItem(1, "Widget")},
			})
		case http.MethodPost:
			if r.Header.Get("Idempotency-Key") == "" {
				writeProblem(w, http.StatusBadRequest, "Idempotency-Key header is required")
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				writeProblem(w, http.StatusUnprocessableEntity, "invalid JSON body")
				return
			}
			name, _ := body["name"].(string)
			if name == "" {
				writeProblem(w, http.StatusUnprocessableEntity, "name is required")
				return
			}
			writeJSON(w, http.StatusCreated, sampleItem(2, name))
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/items/export", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/csv")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("id,name\n1,Widget\n"))
	})

	mux.HandleFunc("/items/import", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil || len(raw) == 0 {
			writeProblem(w, http.StatusBadRequest, "request body is required")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("/items/", func(w http.ResponseWriter, r *http.Request) {
		idStr := strings.TrimPrefix(r.URL.Path, "/items/")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeProblem(w, http.StatusNotFound, "no such item: "+idStr)
			return
		}
		switch r.Method {
		case http.MethodGet:
			w.Header().Set("ETag", `"item-`+idStr+`-v1"`)
			w.Header().Set("Cache-Control", "max-age=60")
			writeJSON(w, http.StatusOK, sampleItem(id, "Widget"))
		case http.MethodDelete:
			if r.Header.Get("Idempotency-Key") == "" || r.Header.Get("If-Match") == "" {
				writeProblem(w, http.StatusBadRequest, "Idempotency-Key and If-Match headers are required")
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	return httptest.NewServer(mux)
}

func sampleItem(id int64, name string) map[string]any {
	return map[string]any{
		"id":         id,
		"name":       name,
		"created_at": "2026-01-01T00:00:00Z",
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"title":  http.StatusText(status),
		"status": status,
		"detail": detail,
	})
}
