package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	_ "image/jpeg" // decoders registered to read real image size
	"image/png"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	_ "golang.org/x/image/tiff" // pdfcpu emits grayscale/1-bit images as TIFF
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
			var buf bytes.Buffer
			if _, err := io.Copy(&buf, img); err != nil || buf.Len() == 0 {
				skippedFmt++
				continue
			}
			data, w, h := buf.Bytes(), img.Width, img.Height
			if ext != "PNG" && ext != "JPG" {
				// fpdf embeds only PNG/JPG/GIF; pdfcpu hands grayscale and
				// 1-bit images over as TIFF. Transcode to PNG so the figure is
				// kept instead of silently dropped.
				p, pw, ph, err := transcodePNG(data)
				if err != nil {
					skippedFmt++
					continue
				}
				data, ext, w, h = p, "PNG", pw, ph
			} else if w == 0 || h == 0 {
				// pdfcpu often reports 0x0, so read the size from the bytes.
				cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
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
				Data: data, Ext: ext, W: w, H: h, Page: img.PageNr,
			})
			n++
		}
	}
	if log != nil {
		if skippedFmt > 0 {
			log(fmt.Sprintf("Aviso: %d imágenes en un formato que no se pudo convertir (p. ej. JBIG2/JPX) no se copiaron.", skippedFmt))
		}
		if capped > 0 {
			log(fmt.Sprintf("Aviso: el PDF trae muchas imágenes; se copian %d y se omiten %d.", n, capped))
		}
	}
	return out
}

// transcodePNG decodes an image format fpdf cannot embed and re-encodes it as
// PNG. pdfcpu hands grayscale, RGB and (from separation colour spaces) CMYK
// images over as uncompressed TIFF; x/image/tiff rejects the CMYK ones, so
// bareTIFF reads those directly.
func transcodePNG(data []byte) (out []byte, w, h int, err error) {
	im, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		im, err = bareTIFF(data)
		if err != nil {
			return nil, 0, 0, err
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		return nil, 0, 0, err
	}
	r := im.Bounds()
	return b.Bytes(), r.Dx(), r.Dy(), nil
}

// bareTIFF decodes the uncompressed 8-bit TIFF that pdfcpu emits (1, 3 or 4
// samples per pixel), which x/image/tiff cannot when the colour model is CMYK.
// Anything else returns an error so the caller skips it rather than guessing.
func bareTIFF(data []byte) (image.Image, error) {
	if len(data) < 8 {
		return nil, fmt.Errorf("tiff: too short")
	}
	var bo binary.ByteOrder
	switch string(data[:2]) {
	case "II":
		bo = binary.LittleEndian
	case "MM":
		bo = binary.BigEndian
	default:
		return nil, fmt.Errorf("tiff: not a tiff")
	}
	if bo.Uint16(data[2:]) != 42 {
		return nil, fmt.Errorf("tiff: bad magic")
	}
	ifd := int(bo.Uint32(data[4:]))
	if ifd+2 > len(data) {
		return nil, fmt.Errorf("tiff: bad ifd")
	}
	// tag -> its integer values (SHORT or LONG)
	tags := map[uint16][]int{}
	count := int(bo.Uint16(data[ifd:]))
	for i, p := 0, ifd+2; i < count; i, p = i+1, p+12 {
		if p+12 > len(data) {
			return nil, fmt.Errorf("tiff: truncated ifd")
		}
		tag, typ, n := bo.Uint16(data[p:]), bo.Uint16(data[p+2:]), int(bo.Uint32(data[p+4:]))
		size := 2 // SHORT
		if typ == 4 {
			size = 4 // LONG
		} else if typ != 3 {
			continue // only SHORT/LONG matter for the tags we read
		}
		off := p + 8
		if n*size > 4 {
			off = int(bo.Uint32(data[p+8:]))
		}
		vals := make([]int, 0, n)
		for k := 0; k < n; k++ {
			q := off + k*size
			if q+size > len(data) {
				return nil, fmt.Errorf("tiff: bad tag data")
			}
			if size == 2 {
				vals = append(vals, int(bo.Uint16(data[q:])))
			} else {
				vals = append(vals, int(bo.Uint32(data[q:])))
			}
		}
		tags[tag] = vals
	}
	first := func(t uint16, def int) int {
		if v, ok := tags[t]; ok && len(v) > 0 {
			return v[0]
		}
		return def
	}
	w, h := first(256, 0), first(257, 0)
	spp := first(277, 1)
	if first(259, 1) != 1 { // Compression: only uncompressed
		return nil, fmt.Errorf("tiff: compressed")
	}
	for _, b := range tags[258] { // BitsPerSample: only 8
		if b != 8 {
			return nil, fmt.Errorf("tiff: %d bits/sample", b)
		}
	}
	if w <= 0 || h <= 0 || (spp != 1 && spp != 3 && spp != 4) {
		return nil, fmt.Errorf("tiff: unsupported geometry")
	}
	var pix []byte
	offs, counts := tags[273], tags[279]
	for i, off := range offs {
		c := w * h * spp
		if i < len(counts) {
			c = counts[i]
		}
		if off < 0 || off+c > len(data) {
			return nil, fmt.Errorf("tiff: strip out of range")
		}
		pix = append(pix, data[off:off+c]...)
	}
	if len(pix) < w*h*spp {
		return nil, fmt.Errorf("tiff: short pixel data")
	}
	r := image.Rect(0, 0, w, h)
	switch spp {
	case 1:
		img := image.NewGray(r)
		copy(img.Pix, pix)
		return img, nil
	case 4:
		img := image.NewCMYK(r)
		copy(img.Pix, pix)
		return img, nil
	default: // 3: RGB
		img := image.NewNRGBA(r)
		for i := 0; i < w*h; i++ {
			img.Pix[i*4], img.Pix[i*4+1], img.Pix[i*4+2], img.Pix[i*4+3] =
				pix[i*3], pix[i*3+1], pix[i*3+2], 255
		}
		return img, nil
	}
}

// IsLikelyPageScan reports whether a figure looks like the scan of a whole page
// rather than an illustration inside one.
func (f Figure) IsLikelyPageScan() bool {
	return f.W*f.H >= pageScanPixels
}
