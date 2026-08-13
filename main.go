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
	// A custom value so repeating --key accumulates; flag.String keeps only the
	// last one. The default stays empty on purpose: flag prints defaults, so
	// reading the env var here would leak the key in --help.
	var key keyList
	flag.Var(&key, "key", "clave de DeepL; se guarda. Repite --key para añadir varias")
	source := flag.String("source", "", "idioma de origen; vacío = detectar solo. Ej: EN")
	target := flag.String("target", "ES", "idioma de destino. Ej: ES")
	mode := flag.String("mode", "translation", "translation = solo la traducción · bilingual = original y traducción")
	verbose := flag.Bool("verbose", false, "mostrar el avance con hora en cada paso")
	quiet := flag.Bool("quiet", false, "no mostrar el avance, solo errores")
	port := flag.Int("port", 8799, "puerto de la interfaz web")
	ollama := flag.String("ollama", "", "modelo local de Ollama para cuando se agote la cuota de DeepL (ej: qwen2.5)")
	flag.Usage = usage
	flag.Parse()

	// A key given on the command line is added to the stored list rather than
	// replacing it, so a second account can take over when the first runs out.
	// Precedence: --key and the env var are added; whatever is stored is used.
	if len(key) > 0 {
		addKeys(key)
	}
	if k := strings.TrimSpace(os.Getenv("BILINGUA_DEEPL_KEY")); k != "" {
		addKeys(splitKeys(k))
	}
	if *ollama != "" {
		os.Setenv("BILINGUA_OLLAMA_MODEL", *ollama)
	}
	// Adding keys is a job of its own: without a PDF, save and report instead of
	// opening the browser.
	if len(key) > 0 && *in == "" {
		stored := loadKeys()
		fmt.Printf("✓ Guardada. Tienes %d clave(s) en %s\n", len(stored), keyPath())
		if len(stored) > 1 {
			fmt.Println("  Cuando la primera agote su cuota mensual, seguirá con la siguiente.")
		}
		fmt.Println("\nAhora ya puedes traducir:  bilingua --in libro.pdf --verbose")
		return
	}
	keys := strings.Join(loadKeys(), "\n")

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
		if keys == "" && strings.TrimSpace(os.Getenv("BILINGUA_OLLAMA_MODEL")) == "" {
			fmt.Fprintln(os.Stderr, "Falta la clave de DeepL.")
			fmt.Fprintln(os.Stderr, "  Consíguela gratis en https://www.deepl.com/pro-api y pásala una vez con --key TU_CLAVE")
			fmt.Fprintln(os.Stderr, "  (o traduce con un modelo local: --ollama qwen2.5).")
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
		if fi, err := os.Stat(o); err == nil && fi.IsDir() {
			fmt.Fprintf(os.Stderr, "%s es una carpeta; indica un archivo .pdf de salida con --out.\n", o)
			os.Exit(1)
		}
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
		if n := len(loadKeys()); n > 1 {
			logf(fmt.Sprintf("Bilingua — %s → %s, %s · %d claves (si una se agota, sigue con la siguiente)",
				orAuto(*source), strings.ToUpper(*target), modeName(*mode), n))
		} else {
			logf(fmt.Sprintf("Bilingua — %s → %s, %s", orAuto(*source), strings.ToUpper(*target), modeName(*mode)))
		}
		if err := TranslateBook(*in, o, keys, *source, *target, *mode, logf); err != nil {
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

// keyList lets --key be repeated; each use adds a key.
type keyList []string

func (k *keyList) String() string { return "" }

func (k *keyList) Set(v string) error {
	*k = append(*k, splitKeys(v)...)
	return nil
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

Claves     Se piden una sola vez con --key y quedan guardadas en este equipo.
           Puedes tener VARIAS: cuando una agota su cuota mensual, sigue con la
           siguiente sin perder lo ya traducido.
             bilingua --key PRIMERA:fx --key SEGUNDA:fx   (guarda las dos y sale)
           Se guardan en el archivo de abajo, una por línea; puedes editarlo.
           Gratis (500.000 caracteres/mes cada una) en https://www.deepl.com/pro-api

Sin cuota   Cuando se agotan todas las claves puede seguir con un modelo LOCAL
            (Ollama), gratis e ilimitado pero más lento y solo para libros de
            texto (no escaneados):
              ollama serve         (en otra terminal, una vez)
              bilingua --in libro.pdf --key TU_CLAVE:fx --ollama qwen2.5
            Traduce con DeepL hasta agotar la cuota y de ahí sigue con Ollama.

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
