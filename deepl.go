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
// Several keys can be held at once: when one runs out of monthly quota the
// client moves on to the next instead of stopping the book.
type DeepL struct {
	keys   []string
	idx    int
	client *http.Client
	log    func(string)
	// charsByPage feeds the splitter so a part never exceeds the character
	// ceiling of a document.
	charsByPage []int
	// testEndpoint overrides the API URL in tests.
	testEndpoint string
}

func NewDeepL(key string) *DeepL {
	return NewDeepLKeys(splitKeys(key))
}

func NewDeepLKeys(keys []string) *DeepL {
	return &DeepL{keys: keys, client: &http.Client{Timeout: 60 * time.Second}}
}

// key returns the key in use right now.
func (d *DeepL) key() string {
	if d.idx < len(d.keys) {
		return d.keys[d.idx]
	}
	return ""
}

func (d *DeepL) keyCount() int { return len(d.keys) }

// nextKey switches to the following key and reports whether there was one.
func (d *DeepL) nextKey(reason string) bool {
	if d.idx+1 >= len(d.keys) {
		return false
	}
	d.idx++
	if d.log != nil {
		d.log(fmt.Sprintf("La clave %d %s; sigo con la clave %d de %d.", d.idx, reason, d.idx+1, len(d.keys)))
	}
	return true
}

func (d *DeepL) endpoint() string {
	if d.testEndpoint != "" {
		return d.testEndpoint
	}
	if strings.HasSuffix(d.key(), ":fx") {
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
	if d.key() == "" {
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
		req.Header.Set("Authorization", "DeepL-Auth-Key "+d.key())
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
		// A used-up or rejected key is not the end of the book when another one
		// is configured: switch and retry the very same batch.
		if resp.StatusCode == 456 {
			if d.nextKey("agotó su cuota mensual") {
				attempt = -1
				continue
			}
			return nil, fmt.Errorf("se agotó la cuota mensual de %s (error 456). "+
				"Añade otra clave con --key OTRA_CLAVE y vuelve a ejecutar: se retoma donde quedó", plural(len(d.keys), "tu clave", "todas tus claves"))
		}
		if resp.StatusCode == 401 || resp.StatusCode == 403 {
			if d.nextKey("fue rechazada") {
				attempt = -1
				continue
			}
			return nil, fmt.Errorf("DeepL rechazó la clave. Comprueba las guardadas en %s "+
				"o añade otra con --key TU_CLAVE", keyPath())
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

func plural(n int, one, many string) string {
	if n <= 1 {
		return one
	}
	return many
}

// Usage returns characters used/limit for one key (free tier = 500000/month).
func (d *DeepL) usageOf(key string) (used, limit int64, err error) {
	ep := "https://api-free.deepl.com/v2/usage"
	if !strings.HasSuffix(key, ":fx") {
		ep = "https://api.deepl.com/v2/usage"
	}
	form := url.Values{}
	form.Set("auth_key", key)
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

// Usage adds up what is left across every configured key, which is what decides
// whether a book fits.
func (d *DeepL) Usage() (used, limit int64, err error) {
	var last error
	ok := false
	for _, k := range d.keys {
		u, l, e := d.usageOf(k)
		if e != nil {
			last = e
			continue
		}
		used += u
		limit += l
		ok = true
	}
	if !ok {
		return 0, 0, last
	}
	return used, limit, nil
}
