package main

import (
	"bytes"
	"image"
	_ "image/jpeg" // decoders registered to read real image size
	_ "image/png"
	"io"
	"os"
	"regexp"
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

// Safety cap: a pathological PDF can carry thousands of image fragments.
const maxFigures = 400

// Figure or table caption, Spanish and English, arabic or roman numbering.
// Deliberately not anchored: the text extractor often returns a whole page as a
// single paragraph, leaving the caption mid-block.
var captionRe = regexp.MustCompile(`(?i)\b(fig(?:ure|ura|\.)|plate|l[áa]mina|tabla|cuadro|ilustraci[óo]n)\s*\.?\s*(\d{1,3}|[ivxlc]{1,6})\b`)

// IsCaption reports whether a paragraph contains a figure caption.
func IsCaption(p string) bool {
	return captionRe.MatchString(strings.TrimSpace(p))
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

	n := 0
	for _, byObj := range pages {
		for _, img := range byObj {
			if n >= maxFigures {
				if log != nil {
					log("Aviso: el PDF trae demasiadas imágenes; se incluyen las primeras.")
				}
				return out
			}
			if img.IsImgMask {
				continue
			}
			ext := strings.ToUpper(strings.TrimPrefix(img.FileType, "."))
			if ext == "JPEG" {
				ext = "JPG"
			}
			if ext != "PNG" && ext != "JPG" {
				continue
			}
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, img); err != nil || buf.Len() == 0 {
				continue
			}
			// pdfcpu often reports 0x0, so read the size from the image itself.
			w, h := img.Width, img.Height
			if w == 0 || h == 0 {
				cfg, _, err := image.DecodeConfig(bytes.NewReader(buf.Bytes()))
				if err != nil {
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
	return out
}
