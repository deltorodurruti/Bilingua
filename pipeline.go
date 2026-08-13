package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// A page holding this many characters counts as carrying real text.
const textPageMinChars = 100

// TranslateBook is the shared core used by both the CLI and the web GUI.
// log receives human-readable progress lines.
func TranslateBook(inPath, outPath, key, source, target, mode string, log func(string)) error {
	log("Abriendo el PDF…")
	byPage, unreadable, err := ExtractParagraphsByPage(inPath)
	if err != nil {
		return err
	}
	var paras []string
	for _, ps := range byPage {
		paras = append(paras, ps...)
	}

	// Figures are read before deciding the route: image-only pages are placed
	// as figures by the text route, so their images survive it.
	figsByPage := ExtractFigures(inPath, log)

	var totalChars, textPages, scannedPages int
	charsByPage := make([]int, len(byPage))
	for i, ps := range byPage {
		n := 0
		for _, p := range ps {
			n += utf8.RuneCountInString(p)
		}
		charsByPage[i] = n
		totalChars += n
		if n >= textPageMinChars {
			textPages++
			continue
		}
		for _, fg := range figsByPage[i+1] {
			if fg.IsLikelyPageScan() {
				scannedPages++
				break
			}
		}
	}

	// DeepL's document endpoint runs OCR but reflows the whole book and drops
	// some of its images; over the per-document character ceiling it also splits
	// the file and cuts a paragraph at the seam. So it is used ONLY for a genuine
	// scan — where almost no page carries an extractable text layer. A few
	// image-only pages in an otherwise-text book are plates or illustrations, not
	// scanned text: the text route below places them as figures, keeping every
	// image and adding no seam.
	if len(byPage) == 0 || textPages*2 < len(byPage) {
		if textPages > 0 {
			log(fmt.Sprintf("Solo %d de %d páginas traen texto seleccionable: trato el libro como escaneado y lo mando al OCR de DeepL.", textPages, len(byPage)))
		} else {
			log("Este PDF parece escaneado (sin texto seleccionable). Lo traduzco con el OCR de DeepL, conservando el diseño…")
		}
		d := NewDeepL(key)
		d.log = log
		// A genuine scan has no extractable text, so totalChars is ~0 and cannot
		// gauge how much the OCR will produce. Estimate it from the page count so
		// a long scan is split instead of being rejected whole by DeepL.
		const estCharsPerScanPage = 2000
		est := make([]int, len(byPage))
		estTotal := 0
		for j := range est {
			est[j] = charsByPage[j]
			if est[j] < estCharsPerScanPage {
				est[j] = estCharsPerScanPage
			}
			estTotal += est[j]
		}
		d.charsByPage = est
		tooBig := false
		if fi, serr := os.Stat(inPath); serr == nil && fi.Size() > DocSizeLimit(key) {
			tooBig = true
		}
		if int64(estTotal) > DocCharLimit(key) {
			log(fmt.Sprintf("El libro rondaría %s caracteres al pasar por OCR y un documento admite %s: lo divido en partes.",
				thousands(int64(estTotal)), thousands(DocCharLimit(key))))
			tooBig = true
		}
		if tooBig {
			if err := d.TranslateLargeScan(inPath, outPath, source, target, log); err != nil {
				return fmt.Errorf("error traduciendo el escaneo grande: %w", err)
			}
		} else if err := d.TranslateDocument(inPath, outPath, source, target, log); err != nil {
			return fmt.Errorf("error traduciendo el escaneo: %w", err)
		}
		log("✓ Listo (escaneo traducido con OCR): " + outPath)
		return nil
	}
	log(fmt.Sprintf("Texto extraído: %d párrafos, ~%d caracteres.", len(paras), totalChars))
	// The text route keeps every image (as a figure) and adds no seam. Only warn
	// about pages that carried no readable text, so nothing thins out silently.
	if len(unreadable) > 0 {
		log(fmt.Sprintf("Aviso: %d páginas (%s…) no traían texto legible; si tenían texto escaneado, revísalas en el original.",
			len(unreadable), pageList(unreadable)))
	}
	if scannedPages > 0 {
		log(fmt.Sprintf("%d páginas son imágenes a página completa: las conservo como figuras (si alguna fuese texto escaneado, ese texto quedaría sin traducir; revísalas).", scannedPages))
	}

	ol := ollamaFromEnv()
	d := NewDeepL(key)
	d.log = log
	if used, limit, uerr := d.Usage(); uerr == nil && limit > 0 {
		if n := d.keyCount(); n > 1 {
			log(fmt.Sprintf("Cuenta DeepL (%d claves sumadas): %d/%d caracteres usados este mes.", n, used, limit))
		} else {
			log(fmt.Sprintf("Cuenta DeepL: %d/%d caracteres usados este mes.", used, limit))
		}
		if int64(totalChars) > (limit - used) {
			if ol == nil {
				// Starting anyway would spend what is left and still fail partway.
				return fmt.Errorf("este libro necesita ~%d caracteres y en tu cuota mensual quedan %d.\n"+
					"Espera al próximo ciclo, usa otra clave, o parte el PDF en tramos más chicos", totalChars, limit-used)
			}
			log("La cuota de DeepL no alcanza para todo el libro; lo que falte lo traduce Ollama (local, gratis, más lento).")
		}
	}

	// Requests must stay under DeepL's limits: 50 text params and 128 KiB total.
	const maxParams = 45
	const maxBytes = 60000
	partial := outPath + ".parcial.json"
	tag := strings.ToUpper(source) + "|" + strings.ToUpper(target) + "|" + mode
	translations := loadPartial(partial, paras, tag)
	if len(translations) > 0 {
		log(fmt.Sprintf("Retomo una traducción anterior: %d de %d párrafos ya estaban hechos.", len(translations), len(paras)))
	}
	useOllama := d.key() == "" && ol != nil
	i := len(translations)
	for i < len(paras) {
		end := i
		bytes := 0
		for end < len(paras) && (end-i) < maxParams && bytes+len(paras[end]) < maxBytes {
			bytes += len(paras[end])
			end++
		}
		if end == i { // a single oversized paragraph — send it alone
			end = i + 1
		}
		via := "DeepL"
		if useOllama {
			via = "Ollama"
		}
		log(fmt.Sprintf("Traduciendo párrafos %d–%d de %d (%s)…", i+1, end, len(paras), via))
		var out []string
		var err error
		if useOllama {
			out, err = ol.Translate(paras[i:end], source, target)
		} else {
			out, err = d.Translate(paras[i:end], source, target)
			if err != nil && ol != nil && errors.Is(err, ErrQuota) {
				log("Se agotó la cuota de DeepL; sigo con Ollama (" + ol.model + "), local y gratis pero más lento…")
				useOllama = true
				out, err = ol.Translate(paras[i:end], source, target)
			}
		}
		if err != nil {
			// Keep what was already done, so a retry resumes here.
			savePartial(partial, paras, translations, tag)
			return fmt.Errorf("error traduciendo: %w\n"+
				"Lo traducido hasta aquí queda guardado: vuelve a ejecutar el mismo comando y sigue desde el párrafo %d", err, i+1)
		}
		translations = append(translations, out...)
		i = end
		// Checkpoint after every batch: an interruption (Ctrl-C, crash) must not
		// discard work already paid for.
		savePartial(partial, paras, translations, tag)
	}

	// One translation per paragraph sent; a mismatch would misalign the whole
	// book, so fail instead of writing it.
	if len(translations) != len(paras) {
		return fmt.Errorf("desajuste de conteo (%d párrafos, %d traducciones): no se escribe un PDF incompleto", len(paras), len(translations))
	}

	figsBefore := placeFigures(byPage, figsByPage)
	if n := countFigures(figsBefore); n > 0 {
		log(fmt.Sprintf("Figuras encontradas: %d (se copian junto a su leyenda).", n))
	}

	title := strings.TrimSuffix(filepath.Base(inPath), filepath.Ext(inPath))
	log("Generando el PDF de salida…")
	if err := WritePDFWithFigures(outPath, title, mode, paras, translations, figsBefore, log); err != nil {
		return err
	}
	os.Remove(partial)
	log("✓ Listo: " + outPath)
	return nil
}

type partialFile struct {
	Paragraphs   int      `json:"paragraphs"`
	Fingerprint  string   `json:"fingerprint"`
	Translations []string `json:"translations"`
}

// fingerprint ties a saved partial translation to the exact text AND the target
// it came from, so resuming with another --target does not reuse the wrong one.
func fingerprint(paras []string, tag string) string {
	n := 0
	for _, p := range paras {
		n += len(p)
	}
	head, tail := "", ""
	if len(paras) > 0 {
		head = paras[0]
		tail = paras[len(paras)-1]
		if len(head) > 60 {
			head = head[:60]
		}
		if len(tail) > 60 {
			tail = tail[:60]
		}
	}
	return fmt.Sprintf("%s|%d/%d/%s|%s", tag, len(paras), n, head, tail)
}

func loadPartial(path string, paras []string, tag string) []string {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var p partialFile
	if json.Unmarshal(b, &p) != nil || p.Fingerprint != fingerprint(paras, tag) || len(p.Translations) > len(paras) {
		os.Remove(path)
		return nil
	}
	return p.Translations
}

func savePartial(path string, paras, translations []string, tag string) {
	b, err := json.Marshal(partialFile{
		Paragraphs:   len(paras),
		Fingerprint:  fingerprint(paras, tag),
		Translations: translations,
	})
	if err == nil {
		_ = os.WriteFile(path, b, 0o600)
	}
}

func pageList(pages []int) string {
	var b strings.Builder
	for i, p := range pages {
		if i == 5 {
			break
		}
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%d", p)
	}
	return b.String()
}

// placeFigures maps paragraph index -> figures to draw just before it, so a
// plate lands on top of its caption. Captions on their own line win; a loose
// match is the fallback. Extra figures go to the start of their page, figures on
// text-less pages carry over, and anything left is emitted at the end (key -1):
// none is ever dropped.
func placeFigures(byPage [][]string, figsByPage map[int][]Figure) map[int][]Figure {
	if len(figsByPage) == 0 {
		return nil
	}
	out := map[int][]Figure{}
	idx := 0
	var pending []Figure
	for p, paras := range byPage {
		figs := append(pending, figsByPage[p+1]...) // byPage[0] is page 1
		pending = nil
		if len(paras) == 0 {
			pending = figs
			continue
		}
		if len(figs) == 0 {
			idx += len(paras)
			continue
		}
		caps := make([]int, 0, len(figs))
		for i, para := range paras {
			if IsAnchoredCaption(para) {
				caps = append(caps, idx+i)
			}
		}
		if len(caps) == 0 {
			for i, para := range paras {
				if IsCaption(para) {
					caps = append(caps, idx+i)
				}
			}
		}
		for i, fg := range figs {
			if i < len(caps) {
				out[caps[i]] = append(out[caps[i]], fg)
			} else {
				out[idx] = append(out[idx], fg)
			}
		}
		idx += len(paras)
	}
	if len(pending) > 0 {
		out[-1] = append(out[-1], pending...)
	}
	return out
}

func countFigures(m map[int][]Figure) int {
	n := 0
	for _, v := range m {
		n += len(v)
	}
	return n
}
