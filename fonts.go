package main

import _ "embed"

// Fuente Unicode incrustada (Noto Sans, licencia OFL). Cubre acentos, comillas
// curvas, guiones largos, diacríticos de transliteración y griego, para que el
// PDF no muestre "letras raras" como pasaba con la fuente latin-1.
//
//go:embed fonts/NotoSans-Regular.ttf
var notoRegular []byte

//go:embed fonts/NotoSans-Bold.ttf
var notoBold []byte

//go:embed fonts/NotoSans-Italic.ttf
var notoItalic []byte
