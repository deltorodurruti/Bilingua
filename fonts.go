package main

import _ "embed"

// Embedded Unicode font (Noto Sans, OFL). Covers accents, curly quotes, dashes,
// transliteration diacritics and Greek, which the latin-1 default mangled.
//
//go:embed fonts/NotoSans-Regular.ttf
var notoRegular []byte

//go:embed fonts/NotoSans-Bold.ttf
var notoBold []byte

//go:embed fonts/NotoSans-Italic.ttf
var notoItalic []byte
