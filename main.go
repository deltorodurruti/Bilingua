// Bilingua — traductor de libros PDF (EN→ES y otros). GUI web local + CLI.
// Un solo binario, sin dependencias nativas. Motor: DeepL.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"time"
)

//go:embed web/index.html
var indexHTML []byte

func main() {
	cli := flag.Bool("cli", false, "modo línea de comandos (sin interfaz)")
	in := flag.String("in", "", "PDF de entrada (modo --cli)")
	out := flag.String("out", "", "PDF de salida (modo --cli)")
	key := flag.String("key", os.Getenv("BILINGUA_DEEPL_KEY"), "clave de DeepL (o variable BILINGUA_DEEPL_KEY)")
	source := flag.String("source", "", "idioma origen (vacío = detectar). Ej: EN")
	target := flag.String("target", "ES", "idioma destino. Ej: ES")
	mode := flag.String("mode", "bilingual", "bilingual (original+traducción) o translation (solo traducción)")
	port := flag.Int("port", 8799, "puerto de la interfaz web")
	flag.Parse()

	if *cli {
		if *in == "" {
			fmt.Println("Uso: bilingua --cli --in libro.pdf [--out salida.pdf] --key TU_CLAVE [--source EN --target ES --mode bilingual]")
			os.Exit(1)
		}
		o := *out
		if o == "" {
			o = *in + ".traducido.pdf"
		}
		err := TranslateBook(*in, o, *key, *source, *target, *mode, func(s string) { fmt.Println("· " + s) })
		if err != nil {
			log.Fatalf("Error: %v", err)
		}
		return
	}

	// modo GUI: servidor local + navegador
	s := NewServer()
	addr := fmt.Sprintf("127.0.0.1:%d", *port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		log.Fatalf("No se pudo abrir el puerto %d: %v", *port, err)
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
