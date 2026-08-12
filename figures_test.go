package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"testing"

	"github.com/go-pdf/fpdf"
)

func TestIsCaption(t *testing.T) {
	yes := []string{
		"Fig. 3 — el sello",
		"Figura IV. La rueda",
		"Plate 2: the seal",
		"Tabla 1. Correspondencias",
		"Lámina 7",
		"…la página entera como un solo bloque. Fig. 1 - the vessel of the work.",
	}
	no := []string{"El operador limpia el espacio.", "3 de mayo", "Configuración del altar"}
	for _, s := range yes {
		if !IsCaption(s) {
			t.Errorf("debía reconocerse como leyenda: %q", s)
		}
	}
	for _, s := range no {
		if IsCaption(s) {
			t.Errorf("no debía reconocerse como leyenda: %q", s)
		}
	}
}

// placeFigures must never drop a figure.
func TestPlaceFiguresNoPierdeNinguna(t *testing.T) {
	byPage := [][]string{
		{"Texto de la página uno.", "Fig. 1 — el diagrama"},
		{"Texto de la página dos."},
		nil, // image-only page
	}
	figs := map[int][]Figure{
		1: {{Ext: "PNG", Page: 1}},
		2: {{Ext: "PNG", Page: 2}},
		3: {{Ext: "PNG", Page: 3}},
	}
	got := placeFigures(byPage, figs)
	if n := countFigures(got); n != 3 {
		t.Fatalf("se perdieron figuras: %d de 3", n)
	}
	if len(got[1]) != 1 {
		t.Errorf("la figura de la página 1 debía pegarse a su leyenda (párrafo 1): %v", got)
	}
}

// A PDF with one figure: it is extracted and embedded in the output.
func TestExtraeYPegaFigura(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 500, 360))
	for y := 0; y < 360; y++ {
		for x := 0; x < 500; x++ {
			img.Set(x, y, color.RGBA{uint8(x / 2), 90, uint8(y / 2), 255})
		}
	}
	var pb bytes.Buffer
	if err := png.Encode(&pb, img); err != nil {
		t.Fatal(err)
	}
	src := fpdf.New("P", "mm", "A4", "")
	src.AddPage()
	src.SetFont("Helvetica", "", 12)
	src.MultiCell(0, 6, "On Purification. The operator cleanses the space with water and salt.", "", "L", false)
	opt := fpdf.ImageOptions{ImageType: "PNG"}
	src.RegisterImageOptionsReader("f", opt, bytes.NewReader(pb.Bytes()))
	src.ImageOptions("f", 30, src.GetY()+4, 120, 0, true, opt, 0, "")
	src.Ln(4)
	src.MultiCell(0, 6, "Fig. 1 - the vessel of the work.", "", "L", false)
	in := t.TempDir() + "/con_figura.pdf"
	if err := src.OutputFileAndClose(in); err != nil {
		t.Fatal(err)
	}

	byPage, err := ExtractParagraphsByPage(in)
	if err != nil {
		t.Fatalf("no se pudo extraer el texto: %v", err)
	}
	figs := ExtractFigures(in, nil)
	if countFigures(figs) == 0 {
		t.Fatal("no se extrajo la figura del PDF")
	}

	var paras []string
	for _, ps := range byPage {
		paras = append(paras, ps...)
	}
	tr := make([]string, len(paras))
	for i, p := range paras {
		tr[i] = "[traducido] " + p
	}
	out := t.TempDir() + "/salida.pdf"
	if err := WritePDFWithFigures(out, "Prueba", "translation", paras, tr, placeFigures(byPage, figs)); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Size() < 8000 {
		t.Fatalf("la figura no quedó incrustada en la salida (%d bytes)", fi.Size())
	}
}
