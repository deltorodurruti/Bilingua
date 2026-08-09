package main

import (
	"fmt"
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

	// guard against image-only (scanned) PDFs
	var totalChars int
	for _, p := range paras {
		totalChars += len(p)
	}
	if len(paras) == 0 || totalChars < 200 {
		return fmt.Errorf("este PDF no tiene texto legible (parece escaneado). Hay que pasarle OCR antes de traducir")
	}
	log(fmt.Sprintf("Texto extraído: %d párrafos, ~%d caracteres.", len(paras), totalChars))

	d := NewDeepL(key)
	if used, limit, uerr := d.Usage(); uerr == nil && limit > 0 {
		log(fmt.Sprintf("Cuenta DeepL: %d/%d caracteres usados este mes.", used, limit))
		if int64(totalChars) > (limit - used) {
			log("⚠ Aviso: este libro podría superar tu cuota gratuita mensual de DeepL.")
		}
	}

	const batchSize = 40
	translations := make([]string, 0, len(paras))
	for i := 0; i < len(paras); i += batchSize {
		end := i + batchSize
		if end > len(paras) {
			end = len(paras)
		}
		log(fmt.Sprintf("Traduciendo párrafos %d–%d de %d…", i+1, end, len(paras)))
		out, err := d.Translate(paras[i:end], source, target)
		if err != nil {
			return fmt.Errorf("error traduciendo: %w", err)
		}
		translations = append(translations, out...)
	}

	title := strings.TrimSuffix(filepath.Base(inPath), filepath.Ext(inPath))
	log("Generando el PDF de salida…")
	if err := WritePDF(outPath, title, mode, paras, translations); err != nil {
		return err
	}
	log("✓ Listo: " + outPath)
	return nil
}
