package main

import (
	"bytes"
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
	byPage, err := ExtractParagraphsByPage(path)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, ps := range byPage {
		out = append(out, ps...)
	}
	return out, nil
}

// ExtractParagraphsByPage returns paragraphs grouped by page (index 0 = page 1);
// knowing the page is what lets figures sit next to their caption.
// The reader panics on damaged or oddly encrypted PDFs, so that is turned into
// an error: otherwise the whole process died with no message.
func ExtractParagraphsByPage(path string) (pages [][]string, err error) {
	defer func() {
		if r := recover(); r != nil {
			pages, err = nil, fmt.Errorf("el PDF no se pudo leer (¿archivo dañado o protegido?): %v", r)
		}
	}()
	f, r, err := pdf.Open(path)
	if err != nil {
		return nil, fmt.Errorf("no se pudo abrir el PDF: %w", err)
	}
	defer f.Close()

	total := r.NumPage()
	pages = make([][]string, 0, total)
	for i := 1; i <= total; i++ {
		p := r.Page(i)
		if p.V.IsNull() {
			pages = append(pages, nil)
			continue
		}
		txt, err := p.GetPlainText(nil)
		if err != nil {
			pages = append(pages, nil)
			continue
		}
		pages = append(pages, splitParagraphs(txt))
	}
	return pages, nil
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

// WritePDF builds the output PDF. mode: "bilingual" = original + translation,
// "translation" = translation only.
func WritePDF(outPath, title, mode string, originals, translations []string) error {
	return WritePDFWithFigures(outPath, title, mode, originals, translations, nil)
}

// WritePDFWithFigures writes the final PDF. figsBefore maps a paragraph index to
// the figures drawn just before it. A nil map behaves like WritePDF.
func WritePDFWithFigures(outPath, title, mode string, originals, translations []string, figsBefore map[int][]Figure) error {
	pdfDoc := fpdf.New("P", "mm", "A4", "")
	pdfDoc.SetMargins(22, 22, 22)
	pdfDoc.SetAutoPageBreak(true, 20)
	// Embedded Unicode font: UTF-8 goes through untouched, so accents, curly
	// quotes, transliteration diacritics and Greek render correctly.
	pdfDoc.AddUTF8FontFromBytes("Noto", "", notoRegular)
	pdfDoc.AddUTF8FontFromBytes("Noto", "B", notoBold)
	pdfDoc.AddUTF8FontFromBytes("Noto", "I", notoItalic)
	pdfDoc.AliasNbPages("")
	pdfDoc.SetFooterFunc(func() {
		pdfDoc.SetY(-14)
		pdfDoc.SetFont("Noto", "", 8)
		pdfDoc.SetTextColor(172, 176, 184)
		pdfDoc.CellFormat(0, 8, fmt.Sprintf("%d", pdfDoc.PageNo()), "", 0, "C", false, 0, "")
	})
	pdfDoc.AddPage()

	pdfDoc.SetFont("Noto", "B", 20)
	pdfDoc.SetTextColor(30, 34, 45)
	pdfDoc.MultiCell(0, 9, title, "", "C", false)
	pdfDoc.Ln(3)
	pw, _ := pdfDoc.GetPageSize()
	lm, _, rm, _ := pdfDoc.GetMargins()
	y := pdfDoc.GetY()
	pdfDoc.SetDrawColor(74, 111, 165)
	pdfDoc.SetLineWidth(0.4)
	pdfDoc.Line(lm, y, pw-rm, y)
	pdfDoc.Ln(9)

	solo := mode != "bilingual"
	usableW := pw - lm - rm
	for i := range translations {
		for _, fg := range figsBefore[i] {
			drawFigure(pdfDoc, fg, usableW, i)
		}
		if !solo && i < len(originals) {
			pdfDoc.SetFont("Noto", "I", 8)
			pdfDoc.SetTextColor(150, 154, 162)
			pdfDoc.MultiCell(0, 4.2, originals[i], "", "J", false)
			pdfDoc.Ln(1.2)
		}
		pdfDoc.SetFont("Noto", "", 9.5)
		pdfDoc.SetTextColor(26, 28, 34)
		pdfDoc.MultiCell(0, 5.4, translations[i], "", "J", false)
		pdfDoc.Ln(3.6)
	}
	// Figures with nowhere to attach go last rather than being dropped.
	for _, fg := range figsBefore[-1] {
		drawFigure(pdfDoc, fg, usableW, -1)
	}
	if err := pdfDoc.OutputFileAndClose(outPath); err != nil {
		return fmt.Errorf("no se pudo escribir el PDF: %w", err)
	}
	return nil
}

// drawFigure embeds a figure centred and scaled to the usable width.
func drawFigure(pdfDoc *fpdf.Fpdf, fg Figure, usableW float64, idx int) {
	name := fmt.Sprintf("fig_%d_%d_%d", fg.Page, idx, len(fg.Data))
	opt := fpdf.ImageOptions{ImageType: fg.Ext, ReadDpi: false}
	info := pdfDoc.RegisterImageOptionsReader(name, opt, bytes.NewReader(fg.Data))
	if info == nil || pdfDoc.Err() {
		pdfDoc.ClearError() // an unreadable figure must not abort the book
		return
	}
	w, h := info.Extent()
	if w <= 0 || h <= 0 {
		return
	}
	const maxH = 180.0 // mm, leaves room for text on A4
	scale := 1.0
	if w > usableW {
		scale = usableW / w
	}
	if h*scale > maxH {
		scale = maxH / h
	}
	w, h = w*scale, h*scale
	x := (pdfDoc.GetX()*0 + (usableW-w)/2) + func() float64 { l, _, _, _ := pdfDoc.GetMargins(); return l }()
	pdfDoc.Ln(2)
	pdfDoc.ImageOptions(name, x, pdfDoc.GetY(), w, h, true, opt, 0, "")
	pdfDoc.Ln(2.5)
}
