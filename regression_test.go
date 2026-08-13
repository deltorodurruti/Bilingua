package main

import (
	"os"
	"strings"
	"testing"
	"unicode/utf8"
)

// A hard split must never cut a rune in half: the broken bytes used to become
// U+FFFD and silently corrupt the text on its way to DeepL.
func TestSplitParagraphsKeepsValidUTF8(t *testing.T) {
	long := strings.Repeat("ñandú ávido über — ", 700) // well past the 4000 byte cut
	for _, p := range splitParagraphs(long) {
		if !utf8.ValidString(p) {
			t.Fatalf("el corte produjo UTF-8 inválido")
		}
		if strings.ContainsRune(p, '�') {
			t.Fatalf("el corte produjo el carácter de reemplazo U+FFFD")
		}
	}
	joined := strings.Join(splitParagraphs(long), "")
	if utf8.RuneCountInString(joined) < utf8.RuneCountInString(strings.TrimSpace(long))-50 {
		t.Fatalf("el corte perdió texto: %d runas frente a %d", utf8.RuneCountInString(joined), utf8.RuneCountInString(long))
	}
}

// A partial translation is only resumed when it belongs to the same text.
func TestPartialResumeOnlyMatchingText(t *testing.T) {
	path := t.TempDir() + "/parcial.json"
	paras := []string{"uno", "dos", "tres"}
	savePartial(path, paras, []string{"one", "two"}, "EN|ES|translation")

	if got := loadPartial(path, paras, "EN|ES|translation"); len(got) != 2 {
		t.Fatalf("debía retomar 2 párrafos, obtuvo %d", len(got))
	}
	// Different book, same output name: the partial must be discarded.
	if got := loadPartial(path, []string{"otro", "libro", "distinto"}, "EN|ES|translation"); got != nil {
		t.Fatalf("no debía reutilizar una traducción de otro texto: %v", got)
	}
	if _, err := os.Stat(path); err == nil {
		t.Errorf("el parcial que no calza debía borrarse")
	}
}

// The resume directory must be wiped when the job it belongs to changes.
func TestWorkDirManifest(t *testing.T) {
	dir := t.TempDir()
	in := dir + "/libro.pdf"
	if err := os.WriteFile(in, []byte("%PDF-1.4 fake"), 0o644); err != nil {
		t.Fatal(err)
	}
	work := dir + "/salida.pdf.partes"

	if err := ensureWorkDir(work, in, "EN", "ES", 10, freeDocLimit); err != nil {
		t.Fatal(err)
	}
	marker := work + "/done-000-1-5.pdf"
	if err := os.WriteFile(marker, []byte("parte traducida al español"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Same job: the finished part survives so it can be reused.
	if err := ensureWorkDir(work, in, "EN", "ES", 10, freeDocLimit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Errorf("con el mismo trabajo la parte debía conservarse")
	}

	// Different target language: reusing Spanish parts for a French job would
	// produce a wrong book, so the directory must start clean.
	if err := ensureWorkDir(work, in, "EN", "FR", 10, freeDocLimit); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Errorf("al cambiar de idioma las partes viejas debían descartarse")
	}
}

// Captions on their own line win over cross references inside a sentence.
func TestAnchoredCaptionBeatsCrossReference(t *testing.T) {
	if IsAnchoredCaption("como se ve en la figura 4, el sello es doble") {
		t.Errorf("una referencia dentro de la frase no es una leyenda anclada")
	}
	if !IsAnchoredCaption("Figura 4. El sello doble") {
		t.Errorf("una leyenda a principio de línea sí debía reconocerse")
	}
	if !IsCaption("como se ve en la figura 4, el sello es doble") {
		t.Errorf("la forma laxa sigue sirviendo de respaldo")
	}
}

// Resuming with a different target language must not reuse the partial.
func TestPartialResumeRejectsOtherTarget(t *testing.T) {
	path := t.TempDir() + "/p.json"
	paras := []string{"one", "two", "three"}
	savePartial(path, paras, []string{"uno", "dos"}, "EN|ES|translation")
	if got := loadPartial(path, paras, "EN|ES|translation"); len(got) != 2 {
		t.Fatalf("same target should resume, got %d", len(got))
	}
	savePartial(path, paras, []string{"uno", "dos"}, "EN|ES|translation")
	if got := loadPartial(path, paras, "EN|FR|translation"); got != nil {
		t.Fatalf("different target must discard the partial, got %v", got)
	}
}
