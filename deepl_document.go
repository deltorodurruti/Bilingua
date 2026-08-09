package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// docClient tolera subidas/descargas grandes (los timeouts de /translate son cortos).
var docClient = &http.Client{Timeout: 5 * time.Minute}

func (d *DeepL) docEndpoint() string {
	if strings.HasSuffix(d.key, ":fx") {
		return "https://api-free.deepl.com/v2/document"
	}
	return "https://api.deepl.com/v2/document"
}

// TranslateDocument sube un archivo a DeepL, espera la traducción y la escribe en
// outPath. Maneja PDFs escaneados vía el OCR de DeepL y preserva el diseño original.
func (d *DeepL) TranslateDocument(inPath, outPath, source, target string, log func(string)) error {
	if d.key == "" {
		return fmt.Errorf("falta la clave de DeepL")
	}
	id, dkey, err := d.uploadDocument(inPath, source, target)
	if err != nil {
		return err
	}
	for {
		status, secs, msg, err := d.documentStatus(id, dkey)
		if err != nil {
			return err
		}
		if status == "done" {
			break
		}
		if status == "error" {
			if msg == "" {
				msg = "¿escaneo ilegible o formato no soportado?"
			}
			return fmt.Errorf("DeepL no pudo traducir el documento: %s", msg)
		}
		if secs > 0 {
			log(fmt.Sprintf("Traduciendo en DeepL… (~%d s restantes)", secs))
		} else {
			log("Traduciendo el documento en DeepL…")
		}
		time.Sleep(3 * time.Second)
	}
	return d.downloadDocument(id, dkey, outPath)
}

func (d *DeepL) uploadDocument(inPath, source, target string) (id, key string, err error) {
	f, err := os.Open(inPath)
	if err != nil {
		return "", "", err
	}
	defer f.Close()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("target_lang", strings.ToUpper(target))
	if source != "" && strings.ToUpper(source) != "AUTO" {
		_ = mw.WriteField("source_lang", strings.ToUpper(source))
	}
	fw, err := mw.CreateFormFile("file", filepath.Base(inPath))
	if err != nil {
		return "", "", err
	}
	if _, err := io.Copy(fw, f); err != nil {
		return "", "", err
	}
	mw.Close()

	req, _ := http.NewRequest("POST", d.docEndpoint(), &buf)
	req.Header.Set("Authorization", "DeepL-Auth-Key "+d.key)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := docClient.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		var er deeplResp
		_ = json.Unmarshal(body, &er)
		if er.Message != "" {
			return "", "", fmt.Errorf("DeepL %d al subir: %s", resp.StatusCode, er.Message)
		}
		return "", "", fmt.Errorf("DeepL %d al subir: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var r struct {
		DocumentID  string `json:"document_id"`
		DocumentKey string `json:"document_key"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "", "", fmt.Errorf("respuesta ilegible al subir: %w", err)
	}
	return r.DocumentID, r.DocumentKey, nil
}

func (d *DeepL) documentStatus(id, key string) (status string, secs int, msg string, err error) {
	body, _ := json.Marshal(map[string]string{"document_key": key})
	req, _ := http.NewRequest("POST", d.docEndpoint()+"/"+id, bytes.NewReader(body))
	req.Header.Set("Authorization", "DeepL-Auth-Key "+d.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := docClient.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", 0, "", fmt.Errorf("DeepL %d al consultar estado: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	var r struct {
		Status           string `json:"status"`
		SecondsRemaining int    `json:"seconds_remaining"`
		ErrorMessage     string `json:"error_message"`
	}
	_ = json.Unmarshal(b, &r)
	return r.Status, r.SecondsRemaining, r.ErrorMessage, nil
}

func (d *DeepL) downloadDocument(id, key, outPath string) error {
	body, _ := json.Marshal(map[string]string{"document_key": key})
	req, _ := http.NewRequest("POST", d.docEndpoint()+"/"+id+"/result", bytes.NewReader(body))
	req.Header.Set("Authorization", "DeepL-Auth-Key "+d.key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := docClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("DeepL %d al descargar: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}
