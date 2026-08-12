package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeepL client. A free API key ends in ":fx" and uses the free endpoint.
type DeepL struct {
	key    string
	client *http.Client
}

func NewDeepL(key string) *DeepL {
	return &DeepL{key: strings.TrimSpace(key), client: &http.Client{Timeout: 60 * time.Second}}
}

func (d *DeepL) endpoint() string {
	if strings.HasSuffix(d.key, ":fx") {
		return "https://api-free.deepl.com/v2/translate"
	}
	return "https://api.deepl.com/v2/translate"
}

type deeplResp struct {
	Translations []struct {
		Text                   string `json:"text"`
		DetectedSourceLanguage string `json:"detected_source_language"`
	} `json:"translations"`
	Message string `json:"message"`
}

// Translate a batch of text segments. Uses the JSON API (compact, no URL-encode
// inflation). source may be "" for auto-detect. target e.g. "ES", "EN".
func (d *DeepL) Translate(segments []string, source, target string) ([]string, error) {
	if d.key == "" {
		return nil, fmt.Errorf("falta la clave de DeepL")
	}
	payload := map[string]any{
		"text":                segments,
		"target_lang":         strings.ToUpper(target),
		"preserve_formatting": true,
	}
	if source != "" && strings.ToUpper(source) != "AUTO" {
		payload["source_lang"] = strings.ToUpper(source)
	}
	body, _ := json.Marshal(payload)

	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		req, _ := http.NewRequest("POST", d.endpoint(), bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "DeepL-Auth-Key "+d.key)
		resp, err := d.client.Do(req)
		if err != nil {
			lastErr = err
			time.Sleep(time.Duration(attempt+1) * 2 * time.Second)
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode == 429 || resp.StatusCode >= 500 {
			// rate limited or server error: back off and retry
			lastErr = fmt.Errorf("DeepL %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
			time.Sleep(time.Duration(attempt+1) * 3 * time.Second)
			continue
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			return nil, fmt.Errorf("DeepL rechazó la clave. Comprueba la que está guardada en %s "+
				"o pasa otra con --key TU_CLAVE", keyPath())
		}
		if resp.StatusCode == 456 {
			return nil, fmt.Errorf("se agotó tu cuota mensual de DeepL (error 456)")
		}
		if resp.StatusCode != 200 {
			var er deeplResp
			_ = json.Unmarshal(body, &er)
			if er.Message != "" {
				return nil, fmt.Errorf("DeepL %d: %s", resp.StatusCode, er.Message)
			}
			return nil, fmt.Errorf("DeepL %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
		}
		var r deeplResp
		if err := json.Unmarshal(body, &r); err != nil {
			return nil, fmt.Errorf("respuesta ilegible de DeepL: %w", err)
		}
		out := make([]string, len(r.Translations))
		for i, t := range r.Translations {
			out[i] = t.Text
		}
		return out, nil
	}
	return nil, fmt.Errorf("DeepL no respondió tras varios intentos: %w", lastErr)
}

// Usage returns characters used/limit for the key (free tier = 500000/month).
func (d *DeepL) Usage() (used, limit int64, err error) {
	ep := "https://api-free.deepl.com/v2/usage"
	if !strings.HasSuffix(d.key, ":fx") {
		ep = "https://api.deepl.com/v2/usage"
	}
	form := url.Values{}
	form.Set("auth_key", d.key)
	req, _ := http.NewRequest("POST", ep, bytes.NewBufferString(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := d.client.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	var u struct {
		CharacterCount int64 `json:"character_count"`
		CharacterLimit int64 `json:"character_limit"`
	}
	body, _ := io.ReadAll(resp.Body)
	if err := json.Unmarshal(body, &u); err != nil {
		return 0, 0, fmt.Errorf("no se pudo leer el uso: %s", strings.TrimSpace(string(body)))
	}
	return u.CharacterCount, u.CharacterLimit, nil
}
