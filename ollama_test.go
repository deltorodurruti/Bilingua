package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The client must send one request per segment and keep replies aligned.
func TestOllamaTranslateAligned(t *testing.T) {
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req ollamaChatReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		user := req.Messages[len(req.Messages)-1].Content
		seen = append(seen, user)
		_ = json.NewEncoder(w).Encode(ollamaChatResp{Message: ollamaMessage{Content: "T:" + user}})
	}))
	defer srv.Close()

	o := &Ollama{model: "test", host: srv.URL, client: srv.Client()}
	out, err := o.Translate([]string{"uno", "", "dos"}, "EN", "ES")
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 3 || out[0] != "T:uno" || out[1] != "" || out[2] != "T:dos" {
		t.Fatalf("alineación incorrecta: %v", out)
	}
	if len(seen) != 2 { // the empty segment is not sent
		t.Fatalf("debía enviar 2 segmentos no vacíos, envió %d", len(seen))
	}
}

func TestOllamaReportsBadModel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_ = json.NewEncoder(w).Encode(ollamaChatResp{Error: "model not found"})
	}))
	defer srv.Close()
	o := &Ollama{model: "missing", host: srv.URL, client: srv.Client()}
	_, err := o.Translate([]string{"x"}, "EN", "ES")
	if err == nil || !strings.Contains(err.Error(), "pull") {
		t.Fatalf("debía sugerir ollama pull, dio: %v", err)
	}
}
