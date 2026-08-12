// Bilingua translates PDF books (EN->ES and others) through DeepL.
// Single binary, no native dependencies: local web GUI plus CLI.
package main

import (
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

func main() {
	cli := flag.Bool("cli", false, "modo terminal (sin abrir el navegador)")
	in := flag.String("in", "", "PDF a traducir")
	out := flag.String("out", "", "PDF de salida (por defecto: <entrada> (traducido).pdf)")
	// Default stays empty on purpose: flag prints defaults, so reading the env
	// var here would leak the key in --help.
	key := flag.String("key", "", "clave de DeepL (solo la primera vez: queda guardada en el equipo)")
	source := flag.String("source", "", "idioma de origen; vacío = detectar solo. Ej: EN")
	target := flag.String("target", "ES", "idioma de destino. Ej: ES")
	mode := flag.String("mode", "translation", "translation = solo la traducción · bilingual = original y traducción")
	verbose := flag.Bool("verbose", false, "mostrar el avance con hora en cada paso")
	quiet := flag.Bool("quiet", false, "no mostrar el avance, solo errores")
	port := flag.Int("port", 8799, "puerto de la interfaz web")
	flag.Usage = usage
	flag.Parse()

	// Precedence: --key, then env var, then the key stored on this machine.
	if *key == "" {
		*key = strings.TrimSpace(os.Getenv("BILINGUA_DEEPL_KEY"))
	}
	if *key == "" {
		*key = loadKey()
	} else {
		saveKey(*key)
	}

	// --in alone implies terminal mode.
	if *cli || *in != "" {
		if *in == "" {
			fmt.Fprint(os.Stderr, "Falta indicar el PDF:  bilingua --in libro.pdf\n\n")
			usage()
			os.Exit(1)
		}
		if fi, err := os.Stat(*in); err != nil {
			fmt.Fprintf(os.Stderr, "No encuentro el archivo: %s\n", *in)
			os.Exit(1)
		} else if fi.IsDir() {
			fmt.Fprintf(os.Stderr, "%s es una carpeta; indica un PDF.\n", *in)
			os.Exit(1)
		}
		if *key == "" {
			fmt.Fprintln(os.Stderr, "Falta la clave de DeepL.")
			fmt.Fprintln(os.Stderr, "  Consíguela gratis en https://www.deepl.com/pro-api y pásala una vez con --key TU_CLAVE")
			fmt.Fprintln(os.Stderr, "  (queda guardada en este equipo; las próximas veces no hace falta).")
			os.Exit(1)
		}
		if *mode != "translation" && *mode != "bilingual" {
			fmt.Fprintf(os.Stderr, "--mode debe ser translation o bilingual (recibí %q)\n", *mode)
			os.Exit(1)
		}
		o := *out
		if o == "" {
			ext := filepath.Ext(*in)
			o = strings.TrimSuffix(*in, ext) + " (traducido).pdf"
		}
		// Checked up front: finding out at the end costs the DeepL quota already spent.
		if dir := filepath.Dir(o); dir != "" {
			if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
				fmt.Fprintf(os.Stderr, "No puedo guardar en %s: la carpeta no existe.\n", dir)
				os.Exit(1)
			}
		}
		start := time.Now()
		logf := func(s string) {
			switch {
			case *quiet:
			case *verbose:
				fmt.Printf("[%s +%4.0fs] %s\n", time.Now().Format("15:04:05"), time.Since(start).Seconds(), s)
			default:
				fmt.Println("· " + s)
			}
		}
		logf(fmt.Sprintf("Bilingua — %s → %s, %s", orAuto(*source), strings.ToUpper(*target), modeName(*mode)))
		if err := TranslateBook(*in, o, *key, *source, *target, *mode, logf); err != nil {
			fmt.Fprintf(os.Stderr, "\nError: %v\n", err)
			os.Exit(1)
		}
		if !*quiet {
			fmt.Printf("\n✓ Listo en %s → %s\n", time.Since(start).Round(time.Second), o)
		}
		return
	}

	// GUI mode: local server plus browser
	s := NewServer()
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		// A busy port usually means another Bilingua is already running, so
		// point the browser at it. Any other failure is a real error and must
		// not be reported as success.
		if !errors.Is(err, syscall.EADDRINUSE) {
			fmt.Fprintf(os.Stderr, "No pude abrir el puerto %d: %v\n", *port, err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "El puerto %d ya está ocupado (¿otra copia de Bilingua abierta?).\n"+
			"Abro http://%s en el navegador. Si eso no es Bilingua, usa --port OTRO.\n", *port, addr)
		openBrowser("http://" + addr)
		return
	}
	url := "http://" + addr
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║   Bilingua — traductor de libros PDF      ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Println("Abriendo en el navegador:", url)
	fmt.Println("(deja esta ventana abierta mientras traduces; ciérrala para salir)")
	go func() {
		time.Sleep(600 * time.Millisecond)
		openBrowser(url)
	}()
	if err := http.Serve(ln, s.routes()); err != nil {
		log.Fatal(err)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `Bilingua — traduce libros PDF con DeepL (también escaneados, con OCR).

  Interfaz en el navegador:   bilingua
  En la terminal:             bilingua --in libro.pdf

Ejemplos
  bilingua --in libro.pdf                          solo la traducción → "libro (traducido).pdf"
  bilingua --in libro.pdf --verbose                igual, mostrando el avance con hora
  bilingua --in libro.pdf --mode bilingual         original y traducción juntos
  bilingua --in libro.pdf --out ~/Desktop/t.pdf    elegir dónde guardar
  bilingua --in libro.pdf --target EN-US           traducir a otro idioma
  bilingua --port 9000                             interfaz web en otro puerto

Idiomas    ES · EN-US · EN-GB · FR · DE · IT · PT-BR · PT-PT · NL · PL · JA · ZH…
           (--source vacío = detectar solo)

Clave      Se pide una sola vez con --key y queda guardada en este equipo.
           Alternativa sin dejar rastro en el historial del shell:
             export BILINGUA_DEEPL_KEY="tu-clave:fx"
           Gratis (500.000 caracteres/mes) en https://www.deepl.com/pro-api

Opciones
`)
	flag.PrintDefaults()
}

func orAuto(s string) string {
	if strings.TrimSpace(s) == "" {
		return "idioma detectado"
	}
	return strings.ToUpper(s)
}

func modeName(m string) string {
	if m == "bilingual" {
		return "original + traducción"
	}
	return "solo traducción"
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default:
		cmd = "xdg-open"
		args = []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}
