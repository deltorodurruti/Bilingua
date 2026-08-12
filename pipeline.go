package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TranslateBook is the shared core used by both the CLI and the web GUI.
// log receives human-readable progress lines.
func TranslateBook(inPath, outPath, key, source, target, mode string, log func(string)) error {
	log("Abriendo el PDF…")
	byPage, err := ExtractParagraphsByPage(inPath)
	if err != nil {
		return err
	}
	var paras []string
	for _, ps := range byPage {
		paras = append(paras, ps...)
	}

	// Scanned or not is decided by how many pages carry text, not by the total:
	// a hybrid book (300 scanned pages plus 2 with text) cleared the old absolute
	// threshold, took the text path and silently produced a 2-page PDF.
	var totalChars, textPages int
	for _, ps := range byPage {
		n := 0
		for _, p := range ps {
			n += len(p)
		}
		totalChars += n
		if n >= 100 {
			textPages++
		}
	}
	if len(byPage) == 0 || textPages*2 < len(byPage) {
		if textPages > 0 {
			log(fmt.Sprintf("Solo %d de %d páginas traen texto: trato el libro como escaneado y lo mando entero al OCR de DeepL (así no se pierde ninguna página).", textPages, len(byPage)))
		} else {
			log("Este PDF parece escaneado (sin texto seleccionable). Lo traduzco con el OCR de DeepL, conservando el diseño…")
		}
		d := NewDeepL(key)
		if fi, serr := os.Stat(inPath); serr == nil && fi.Size() > DocSizeLimit(key) {
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

	d := NewDeepL(key)
	if used, limit, uerr := d.Usage(); uerr == nil && limit > 0 {
		log(fmt.Sprintf("Cuenta DeepL: %d/%d caracteres usados este mes.", used, limit))
		if int64(totalChars) > (limit - used) {
			log("⚠ Aviso: este libro podría superar tu cuota gratuita mensual de DeepL.")
		}
	}

	// Requests must stay under DeepL's limits: 50 text params and 128 KiB total.
	const maxParams = 45
	const maxBytes = 60000
	translations := make([]string, 0, len(paras))
	i := 0
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
		log(fmt.Sprintf("Traduciendo párrafos %d–%d de %d…", i+1, end, len(paras)))
		out, err := d.Translate(paras[i:end], source, target)
		if err != nil {
			return fmt.Errorf("error traduciendo: %w", err)
		}
		translations = append(translations, out...)
		i = end
	}

	// One translation per paragraph sent; a mismatch would misalign the whole
	// book, so fail instead of writing it.
	if len(translations) != len(paras) {
		return fmt.Errorf("desajuste de conteo (%d párrafos, %d traducciones): no se escribe un PDF incompleto", len(paras), len(translations))
	}

	// Figures are copied from the source and placed next to their caption.
	figsBefore := placeFigures(byPage, ExtractFigures(inPath, log))
	if n := countFigures(figsBefore); n > 0 {
		log(fmt.Sprintf("Figuras encontradas: %d (se copian junto a su leyenda).", n))
	}

	title := strings.TrimSuffix(filepath.Base(inPath), filepath.Ext(inPath))
	log("Generando el PDF de salida…")
	if err := WritePDFWithFigures(outPath, title, mode, paras, translations, figsBefore); err != nil {
		return err
	}
	log("✓ Listo: " + outPath)
	return nil
}

// placeFigures maps paragraph index -> figures to draw just before it, so a
// plate lands on top of its caption. Extra figures go to the start of their
// page, figures on text-less pages carry over, and anything left is emitted at
// the end (key -1): none is ever dropped.
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
			if IsCaption(para) {
				caps = append(caps, idx+i)
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
