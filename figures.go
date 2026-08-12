package main

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg" // decoders registered to read real image size
	_ "image/png"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Figure is an image embedded in the source PDF, copied verbatim into the
// translated one next to its caption.
type Figure struct {
	Data []byte
	Ext  string // PNG or JPG, the formats fpdf embeds
	W, H int
	Page int
}

// Below this side in pixels an image is decoration (rules, bullets, logos).
const minFigurePx = 110

// Above this many pixels an image is most likely a scan of the whole page
// rather than a figure inside it.
const pageScanPixels = 2_000_000

// Safety cap: a pathological PDF can carry thousands of image fragments.
const maxFigures = 400

// Figure or table caption, Spanish and English, arabic or roman numbering.
var captionBody = `(fig(?:ure|ura|\.)|plate|l[áa]mina|tabla|cuadro|ilustraci[óo]n)\s*\.?\s*(\d{1,3}|[ivxlc]{1,6})\b`

// A caption opening its own line is the real thing; the loose form also matches
// cross references inside a sentence and is only used as a fallback, because the
// text extractor often returns a whole page as a single paragraph.
var captionAnchored = regexp.MustCompile(`(?im)^\s*` + captionBody)
var captionLoose = regexp.MustCompile(`(?i)\b` + captionBody)

// IsCaption reports whether a paragraph contains a figure caption.
func IsCaption(p string) bool {
	return captionLoose.MatchString(strings.TrimSpace(p))
}

// IsAnchoredCaption reports whether a paragraph starts a caption line.
func IsAnchoredCaption(p string) bool {
	return captionAnchored.MatchString(p)
}

// ExtractFigures returns the embedded images grouped by page. Unsupported
// resources are skipped rather than failing the whole translation.
func ExtractFigures(path string, log func(string)) map[int][]Figure {
	out := map[int][]Figure{}
	f, err := os.Open(path)
	if err != nil {
		return out
	}
	defer f.Close()

	conf := model.NewDefaultConfiguration()
	conf.UnsupportedResourcePolicy = model.UnsupportedResourceSkip
	pages, err := api.ExtractImagesRaw(f, nil, conf)
	if err != nil && len(pages) == 0 {
		if log != nil {
			log("Aviso: no se pudieron leer las figuras de este PDF; se traduce solo el texto.")
		}
		return out
	}
	if err != nil && log != nil {
		log("Aviso: algunas imágenes no se pudieron leer y no se copiarán.")
	}

	n, skippedFmt, capped := 0, 0, 0
	for _, byObj := range pages {
		// Map order is random in Go; without sorting, figures on the same page
		// came out shuffled and got paired with the wrong caption.
		objs := make([]int, 0, len(byObj))
		for obj := range byObj {
			objs = append(objs, obj)
		}
		sort.Ints(objs)

		for _, obj := range objs {
			img := byObj[obj]
			if img.IsImgMask {
				continue
			}
			if n >= maxFigures {
				capped++
				continue
			}
			ext := strings.ToUpper(strings.TrimPrefix(img.FileType, "."))
			if ext == "JPEG" {
				ext = "JPG"
			}
			if ext != "PNG" && ext != "JPG" {
				skippedFmt++
				continue
			}
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, img); err != nil || buf.Len() == 0 {
				skippedFmt++
				continue
			}
			// pdfcpu often reports 0x0, so read the size from the image itself.
			w, h := img.Width, img.Height
			if w == 0 || h == 0 {
				cfg, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
				if err != nil {
					skippedFmt++
					continue
				}
				w, h = cfg.Width, cfg.Height
			}
			if w < minFigurePx || h < minFigurePx {
				continue
			}
			out[img.PageNr] = append(out[img.PageNr], Figure{
				Data: buf.Bytes(), Ext: ext, W: w, H: h, Page: img.PageNr,
			})
			n++
		}
	}
	if log != nil {
		if skippedFmt > 0 {
			log(fmt.Sprintf("Aviso: %d imágenes en un formato que el PDF de salida no admite (JBIG2/JPX) no se copiaron.", skippedFmt))
		}
		if capped > 0 {
			log(fmt.Sprintf("Aviso: el PDF trae muchas imágenes; se copian %d y se omiten %d.", n, capped))
		}
	}
	return out
}

// IsLikelyPageScan reports whether a figure looks like the scan of a whole page
// rather than an illustration inside one.
func (f Figure) IsLikelyPageScan() bool {
	return f.W*f.H >= pageScanPixels
}
