package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TranslateBook is the shared core used by both the CLI and the web GUI.
// log receives human-readable progress lines (verbose output).
func TranslateBook(inPath, outPath, key, source, target, mode string, log func(string)) error {
	log("Abriendo el PDF…")
	paras, err := ExtractParagraphs(inPath)
	if err != nil {
		return err
	}

	// PDF escaneado (sin texto): lo traduce DeepL con su OCR, preservando el diseño.
	var totalChars int
	for _, p := range paras {
		totalChars += len(p)
	}
	if len(paras) == 0 || totalChars < 200 {
		log("Este PDF parece escaneado (sin texto seleccionable). Lo traduzco con el OCR de DeepL, conservando el diseño…")
		d := NewDeepL(key)
		// DeepL rechaza documentos >10 MB; los escaneos grandes se traducen por partes.
		if fi, serr := os.Stat(inPath); serr == nil && fi.Size() > 9_500_000 {
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

	// Group paragraphs into requests that stay under DeepL's limits:
	// at most 45 text params and ~90 KB of payload per request.
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

	title := strings.TrimSuffix(filepath.Base(inPath), filepath.Ext(inPath))
	log("Generando el PDF de salida…")
	if err := WritePDF(outPath, title, mode, paras, translations); err != nil {
		return err
	}
	log("✓ Listo: " + outPath)
	return nil
}
