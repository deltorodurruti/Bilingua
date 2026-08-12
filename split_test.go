package main

import (
	"fmt"
	"os"
	"testing"

	"github.com/go-pdf/fpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// Parts must each fit the limit, cover every page and never overlap.
func TestPlanPartsCoversEveryPage(t *testing.T) {
	const pages = 30
	doc := fpdf.New("P", "mm", "A4", "")
	for i := 1; i <= pages; i++ {
		doc.AddPage()
		doc.SetFont("Helvetica", "", 12)
		doc.MultiCell(0, 6, fmt.Sprintf("Page %d", i), "", "L", false)
	}
	in := t.TempDir() + "/book.pdf"
	if err := doc.OutputFileAndClose(in); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(in)
	if err != nil {
		t.Fatal(err)
	}

	limit := fi.Size() / 4
	specs, err := planParts(in, t.TempDir(), pages, limit, func(string) {})
	if err != nil {
		t.Fatalf("planParts: %v", err)
	}
	if len(specs) < 2 {
		t.Fatalf("expected several parts, got %d", len(specs))
	}
	next := 1
	for i, sp := range specs {
		if sp.start != next {
			t.Fatalf("part %d starts at %d, expected %d (gap or overlap)", i, sp.start, next)
		}
		if sp.size > limit {
			t.Fatalf("part %d is %d bytes, over the %d limit", i, sp.size, limit)
		}
		n, err := api.PageCountFile(sp.path)
		if err != nil {
			t.Fatal(err)
		}
		if want := sp.end - sp.start + 1; n != want {
			t.Fatalf("part %d has %d pages, expected %d", i, n, want)
		}
		next = sp.end + 1
	}
	if next-1 != pages {
		t.Fatalf("parts cover up to page %d, not %d", next-1, pages)
	}
}

func TestDocSizeLimit(t *testing.T) {
	if got := DocSizeLimit("abc:fx"); got != freeDocLimit {
		t.Errorf("free key should use the free limit, got %d", got)
	}
	if got := DocSizeLimit("abc"); got != proDocLimit {
		t.Errorf("pro key should use the pro limit, got %d", got)
	}
}
