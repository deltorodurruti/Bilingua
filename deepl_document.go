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

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// Document upload limits per DeepL's published usage limits:
// API Free 10 MB, API Pro 100 MB. A free key ends in ":fx".
const (
	freeDocLimit = 10 << 20
	proDocLimit  = 100 << 20
	// Every submitted document is billed a minimum of 50,000 characters, so
	// each extra part costs quota even if it is nearly empty. Parts are made
	// as large as the size limit allows to keep their number down.
	minBilledCharsPerDoc = 50000
	// Stop waiting for a document that DeepL never finishes.
	docTimeout = 45 * time.Minute
)

// Uploads and downloads are slow; /translate timeouts are not enough.
var docClient = &http.Client{Timeout: 10 * time.Minute}

// DocSizeLimit returns the upload limit that applies to this key.
func DocSizeLimit(key string) int64 {
	if strings.HasSuffix(strings.TrimSpace(key), ":fx") {
		return freeDocLimit
	}
	return proDocLimit
}

func (d *DeepL) docEndpoint() string {
	if strings.HasSuffix(d.key, ":fx") {
		return "https://api-free.deepl.com/v2/document"
	}
	return "https://api.deepl.com/v2/document"
}

// TranslateDocument uploads a file, waits for DeepL to translate it (OCR included
// for scanned PDFs, original layout preserved) and writes the result to outPath.
func (d *DeepL) TranslateDocument(inPath, outPath, source, target string, log func(string)) error {
	if d.key == "" {
		return fmt.Errorf("falta la clave de DeepL")
	}
	id, dkey, err := d.uploadDocument(inPath, source, target)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(docTimeout)
	last := ""
	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("DeepL lleva más de %s sin terminar este documento; lo doy por fallido", docTimeout)
		}
		status, secs, msg, err := d.documentStatus(id, dkey)
		if err != nil {
			return err
		}
		switch status {
		case "done":
			return d.downloadDocument(id, dkey, outPath)
		case "error":
			if msg == "" {
				msg = "¿escaneo ilegible o formato no soportado?"
			}
			return fmt.Errorf("DeepL no pudo traducir el documento: %s", msg)
		case "queued", "translating":
		default:
			// An unknown status would otherwise loop forever.
			return fmt.Errorf("DeepL devolvió un estado inesperado (%q); aborto para no quedarme colgado", status)
		}
		line := "Traduciendo el documento en DeepL…"
		if secs > 0 {
			line = fmt.Sprintf("Traduciendo en DeepL… (~%d s restantes)", secs)
		}
		if line != last {
			log(line)
			last = line
		}
		time.Sleep(3 * time.Second)
	}
}

type partSpec struct {
	start, end int
	path       string
	size       int64
}

// planParts splits the PDF into the fewest page ranges that each stay under the
// upload limit. Ranges are measured after trimming, never assumed: pages differ
// wildly in weight (a colour plate can be 8 MB) and pdfcpu copies shared
// resources into every part.
func planParts(inPath, work string, pages int, limit int64, log func(string)) ([]partSpec, error) {
	fi, err := os.Stat(inPath)
	if err != nil {
		return nil, err
	}
	target := int64(float64(limit) * 0.85)
	perPage := fi.Size() / int64(pages)
	if perPage < 1 {
		perPage = 1
	}
	span := int(target / perPage)
	if span < 1 {
		span = 1
	}
	if span > pages {
		span = pages
	}

	var out []partSpec
	for start := 1; start <= pages; {
		end := start + span - 1
		if end > pages {
			end = pages
		}
		got, err := trimFit(inPath, work, start, end, limit, len(out), log)
		if err != nil {
			return nil, err
		}
		out = append(out, got...)
		start = got[len(got)-1].end + 1
	}
	return out, nil
}

// trimFit writes pages [start,end] and, if the result is still too big, halves
// the range until every piece fits.
func trimFit(inPath, work string, start, end int, limit int64, idx int, log func(string)) ([]partSpec, error) {
	path := filepath.Join(work, fmt.Sprintf("part-%03d-%d-%d.pdf", idx, start, end))
	if err := api.TrimFile(inPath, path, []string{fmt.Sprintf("%d-%d", start, end)}, nil); err != nil {
		return nil, fmt.Errorf("no se pudo dividir las páginas %d–%d: %w", start, end, err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if fi.Size() <= limit {
		return []partSpec{{start: start, end: end, path: path, size: fi.Size()}}, nil
	}
	os.Remove(path)
	if start == end {
		return nil, fmt.Errorf("la página %d pesa %d MB y supera el límite de subida de DeepL (%d MB); "+
			"reduce la resolución de esa página y vuelve a intentarlo",
			start, fi.Size()>>20, limit>>20)
	}
	mid := start + (end-start)/2
	left, err := trimFit(inPath, work, start, mid, limit, idx, log)
	if err != nil {
		return nil, err
	}
	right, err := trimFit(inPath, work, mid+1, end, limit, idx+len(left), log)
	if err != nil {
		return nil, err
	}
	return append(left, right...), nil
}

// TranslateLargeScan handles PDFs above the upload limit: it splits them into as
// few parts as possible, translates each and merges the results. Finished parts
// are kept on disk so a failure halfway does not throw away paid work — DeepL
// serves each translated document only once.
func (d *DeepL) TranslateLargeScan(inPath, outPath, source, target string, log func(string)) error {
	pages, err := api.PageCountFile(inPath)
	if err != nil {
		return fmt.Errorf("no se pudieron contar las páginas: %w", err)
	}
	if pages < 1 {
		return fmt.Errorf("el PDF no tiene páginas")
	}
	limit := DocSizeLimit(d.key)

	work := outPath + ".partes"
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}

	log("Preparando las partes (mido cada tramo para que ninguna supere el límite)…")
	specs, err := planParts(inPath, work, pages, limit, log)
	if err != nil {
		return err
	}
	log(fmt.Sprintf("%d páginas → %d partes. Aviso: DeepL cobra un mínimo de %s caracteres por documento, "+
		"así que esto consume al menos %s caracteres de tu cuota.",
		pages, len(specs), thousands(minBilledCharsPerDoc), thousands(int64(len(specs))*minBilledCharsPerDoc)))

	done := make([]string, 0, len(specs))
	for i, sp := range specs {
		want := sp.end - sp.start + 1
		outPart := filepath.Join(work, fmt.Sprintf("done-%03d.pdf", i))
		if n, err := api.PageCountFile(outPart); err == nil && n == want {
			log(fmt.Sprintf("Parte %d/%d (págs %d–%d) ya estaba lista; la reutilizo.", i+1, len(specs), sp.start, sp.end))
			done = append(done, outPart)
			continue
		}
		log(fmt.Sprintf("Traduciendo parte %d/%d — págs %d–%d (%d MB)…", i+1, len(specs), sp.start, sp.end, sp.size>>20))
		if err := d.TranslateDocument(sp.path, outPart, source, target, log); err != nil {
			return fmt.Errorf("error en la parte %d (págs %d–%d): %w\n"+
				"Las partes ya traducidas quedan en %s: vuelve a ejecutar el mismo comando y se reutilizan",
				i+1, sp.start, sp.end, err, work)
		}
		// A returned document is not proof that every page came back.
		if n, err := api.PageCountFile(outPart); err == nil && n != want {
			return fmt.Errorf("la parte %d volvió con %d páginas en vez de %d; no uno un libro incompleto", i+1, n, want)
		}
		done = append(done, outPart)
	}

	if len(done) != len(specs) {
		return fmt.Errorf("faltan partes (%d de %d); no uno un libro incompleto", len(done), len(specs))
	}
	log("Uniendo las partes traducidas…")
	if err := api.MergeCreateFile(done, outPath, false, nil); err != nil {
		return fmt.Errorf("no se pudieron unir las partes: %w", err)
	}
	if n, err := api.PageCountFile(outPath); err == nil && n != pages {
		return fmt.Errorf("el PDF unido tiene %d páginas y el original %d; las partes quedan en %s", n, pages, work)
	}
	os.RemoveAll(work)
	return nil
}

func thousands(n int64) string {
	s := fmt.Sprintf("%d", n)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte('.')
		}
		b.WriteRune(c)
	}
	return b.String()
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
	// Discarding this error left status empty and the caller polling forever.
	if err := json.Unmarshal(b, &r); err != nil {
		return "", 0, "", fmt.Errorf("respuesta ilegible de DeepL al consultar estado: %s", strings.TrimSpace(string(b)))
	}
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
	if _, err := io.Copy(out, resp.Body); err != nil {
		return err
	}
	return out.Sync()
}
