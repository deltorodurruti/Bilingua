package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/go-pdf/fpdf"
	"github.com/ledongthuc/pdf"
)

// ExtractParagraphs reads a PDF and returns its text split into paragraphs.
// Works on PDFs that carry a text layer; scanned PDFs (image-only) return little
// or nothing — the caller should warn the user to OCR first.
func ExtractParagraphs(path string) ([]string, error) {
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir el PDF: %w", err)
	}
	defer f.Close()

	var sb strings.Builder
	total := r.NumPage()
	for i := 1; i <= total; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			continue
		}
		txt, err := p.GetPlainText(nil)
		if err != nil {
			continue
		}
		sb.WriteString(txt)
		sb.WriteString("\n\n")
	}
	return splitParagraphs(sb.String()), nil
}

var multiNL = regexp.MustCompile(`\n{2,}`)
var spaces = regexp.MustCompile(`[ \t]+`)

func splitParagraphs(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	chunks := multiNL.Split(text, -1)
	var out []string
	for _, c := range chunks {
		// join intra-paragraph line breaks into spaces
		c = strings.ReplaceAll(c, "\n", " ")
		c = spaces.ReplaceAllString(c, " ")
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		// hard-split very long paragraphs so DeepL segments stay reasonable
		for len(c) > 4000 {
			cut := 4000
			if idx := strings.LastIndex(c[:4000], ". "); idx > 1000 {
				cut = idx + 1
			}
			out = append(out, strings.TrimSpace(c[:cut]))
			c = strings.TrimSpace(c[cut:])
		}
		out = append(out, c)
	}
	return out
}

// WritePDF builds the output PDF.
// mode: "bilingual" = original + translation per paragraph; "translation" = only translation.
func WritePDF(outPath, title, mode string, originals, translations []string) error {
	pdfDoc := fpdf.New("P", "mm", "A4", "")
	pdfDoc.SetMargins(20, 20, 20)
	pdfDoc.SetAutoPageBreak(true, 20)
	// Fuente Unicode incrustada (Noto Sans): se pasan las cadenas UTF-8 tal cual,
	// sin convertir a latin-1, así los acentos, comillas curvas, guiones largos,
	// diacríticos y griego se ven bien en vez de "letras raras".
	pdfDoc.AddUTF8FontFromBytes("Noto", "", notoRegular)
	pdfDoc.AddUTF8FontFromBytes("Noto", "B", notoBold)
	pdfDoc.AddUTF8FontFromBytes("Noto", "I", notoItalic)
	pdfDoc.AddPage()

	pdfDoc.SetFont("Noto", "B", 17)
	pdfDoc.SetTextColor(20, 20, 25)
	pdfDoc.MultiCell(0, 9, title, "", "L", false)
	pdfDoc.Ln(7)

	for i := range translations {
		if mode == "bilingual" && i < len(originals) {
			pdfDoc.SetFont("Noto", "I", 9.5)
			pdfDoc.SetTextColor(140, 140, 148)
			pdfDoc.MultiCell(0, 5.4, originals[i], "", "L", false)
			pdfDoc.Ln(1.8)
		}
		pdfDoc.SetFont("Noto", "", 11.5)
		pdfDoc.SetTextColor(24, 24, 28)
		pdfDoc.MultiCell(0, 6.4, translations[i], "", "L", false)
		pdfDoc.Ln(5)
	}
	if err := pdfDoc.OutputFileAndClose(outPath); err != nil {
		return fmt.Errorf("no se pudo escribir el PDF: %w", err)
	}
	return nil
}
