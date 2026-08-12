package main

import (
	"os"
	"testing"
)

// The output must use the Unicode font and survive characters that used to
// come out as garbage.
func TestWritePDFUnicode(t *testing.T) {
	orig := []string{`He said "hello" — and left… a naïve café.`}
	trans := []string{`Dijo «hola» —y se fue…: café, ingenuo, ¿qué? Diacríticos ḥ ṣ ṭ ā ī; griego Ἀβρασάξ.`}
	out := "/tmp/bili_font_test.pdf"
	_ = os.Remove(out)
	if err := WritePDF(out, "Prueba — título áéíóú ñ", "bilingual", orig, trans); err != nil {
		t.Fatalf("WritePDF error: %v", err)
	}
	fi, err := os.Stat(out)
	if err != nil {
		t.Fatalf("no se generó el PDF: %v", err)
	}
	if fi.Size() < 1000 {
		t.Fatalf("PDF sospechosamente vacío: %d bytes", fi.Size())
	}
}
