package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Ollama translates text through a local model (a running "ollama serve") as a
// free, unlimited fallback for when the DeepL quota is spent. It handles the
// text route only: a local model cannot OCR a scanned PDF or keep its layout.
type Ollama struct {
	model  string
	host   string
	client *http.Client
}

// ollamaFromEnv builds a client when BILINGUA_OLLAMA_MODEL is set, else nil.
func ollamaFromEnv() *Ollama {
	m := strings.TrimSpace(os.Getenv("BILINGUA_OLLAMA_MODEL"))
	if m == "" {
		return nil
	}
	host := strings.TrimSpace(os.Getenv("BILINGUA_OLLAMA_HOST"))
	if host == "" {
		host = "http://127.0.0.1:11434"
	}
	return &Ollama{
		model:  m,
		host:   strings.TrimRight(host, "/"),
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaChatReq struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  map[string]any  `json:"options"`
}

type ollamaChatResp struct {
	Message ollamaMessage `json:"message"`
	Error   string        `json:"error"`
}

// Translate renders each segment on its own request so the reply stays aligned
// with the input: a local model reordering or merging a batch would misalign
// the whole book. Slower than a batch, but the alignment is guaranteed.
func (o *Ollama) Translate(segments []string, source, target string) ([]string, error) {
	src := "the source language"
	if s := strings.TrimSpace(source); s != "" && strings.ToUpper(s) != "AUTO" {
		src = strings.ToUpper(s)
	}
	sys := fmt.Sprintf("You are a professional literary translator. Translate the user's text from %s to %s. "+
		"Output ONLY the translation: no preamble, no notes, no quotation marks around it. "+
		"Keep the meaning, tone and any line breaks.", src, strings.ToUpper(target))

	out := make([]string, len(segments))
	for i, seg := range segments {
		if strings.TrimSpace(seg) == "" {
			out[i] = seg
			continue
		}
		body, _ := json.Marshal(ollamaChatReq{
			Model:    o.model,
			Stream:   false,
			Options:  map[string]any{"temperature": 0},
			Messages: []ollamaMessage{{Role: "system", Content: sys}, {Role: "user", Content: seg}},
		})
		req, _ := http.NewRequest("POST", o.host+"/api/chat", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		resp, err := o.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("no pude conectar con Ollama en %s (¿está corriendo «ollama serve»?): %w", o.host, err)
		}
		var r ollamaChatResp
		derr := json.NewDecoder(resp.Body).Decode(&r)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			if r.Error != "" {
				return nil, fmt.Errorf("Ollama rechazó la petición: %s (¿tienes el modelo? prueba «ollama pull %s»)", r.Error, o.model)
			}
			return nil, fmt.Errorf("Ollama devolvió HTTP %d", resp.StatusCode)
		}
		if derr != nil {
			return nil, fmt.Errorf("la respuesta de Ollama no se pudo leer: %w", derr)
		}
		out[i] = strings.TrimSpace(r.Message.Content)
	}
	return out, nil
}
