package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Job struct {
	id      string
	logs    chan string
	outPath string
	outName string
	err     error
	done    bool
	mu      sync.Mutex
}

type Server struct {
	jobs map[string]*Job
	mu   sync.Mutex
	tmp  string
}

func newID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func NewServer() *Server {
	tmp, _ := os.MkdirTemp("", "bilingua")
	return &Server{jobs: map[string]*Job{}, tmp: tmp}
}

func (s *Server) routes() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/start", s.handleStart)
	mux.HandleFunc("/events", s.handleEvents)
	mux.HandleFunc("/download", s.handleDownload)
	mux.HandleFunc("/quit", s.handleQuit)
	mux.HandleFunc("/config", s.handleConfig)
	mux.HandleFunc("/savekey", s.handleSaveKey)
	return mux
}

// handleSaveKey persiste la clave apenas se ingresa (sin esperar a traducir).
func (s *Server) handleSaveKey(w http.ResponseWriter, r *http.Request) {
	saveKey(r.URL.Query().Get("key"))
	_, _ = w.Write([]byte("ok"))
}

// handleConfig devuelve la clave de DeepL guardada en el equipo (si hay).
func (s *Server) handleConfig(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"key": loadKey()})
}

// handleQuit lets the GUI shut the program down (no console window to close).
func (s *Server) handleQuit(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("ok"))
	if fl, ok := w.(http.Flusher); ok {
		fl.Flush()
	}
	go func() { time.Sleep(300 * time.Millisecond); os.Exit(0) }()
}

func (s *Server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

func (s *Server) handleStart(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		http.Error(w, "archivo demasiado grande o inválido", 400)
		return
	}
	file, hdr, err := r.FormFile("pdf")
	if err != nil {
		http.Error(w, "falta el PDF", 400)
		return
	}
	defer file.Close()

	key := r.FormValue("key")
	saveKey(key) // recordar la clave para la próxima vez
	source := r.FormValue("source")
	target := r.FormValue("target")
	if target == "" {
		target = "ES"
	}
	mode := r.FormValue("mode")
	if mode == "" {
		mode = "bilingual"
	}

	id := newID()
	inPath := filepath.Join(s.tmp, id+"_in.pdf")
	dst, err := os.Create(inPath)
	if err != nil {
		http.Error(w, "no se pudo guardar el archivo", 500)
		return
	}
	if _, err := io.Copy(dst, file); err != nil {
		dst.Close()
		http.Error(w, "no se pudo guardar el archivo", 500)
		return
	}
	dst.Close()

	base := strings.TrimSuffix(hdr.Filename, filepath.Ext(hdr.Filename))
	outName := base + " (traducido).pdf"
	outPath := filepath.Join(s.tmp, id+"_out.pdf")

	job := &Job{id: id, logs: make(chan string, 256), outPath: outPath, outName: outName}
	s.mu.Lock()
	s.jobs[id] = job
	s.mu.Unlock()

	go func() {
		logf := func(line string) {
			select {
			case job.logs <- line:
			default:
			}
		}
		err := TranslateBook(inPath, outPath, key, source, target, mode, logf)
		job.mu.Lock()
		job.err = err
		job.done = true
		job.mu.Unlock()
		close(job.logs)
	}()

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"job": id})
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("job")
	s.mu.Lock()
	job := s.jobs[id]
	s.mu.Unlock()
	if job == nil {
		http.Error(w, "trabajo no encontrado", 404)
		return
	}
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming no soportado", 500)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	for line := range job.logs {
		fmt.Fprintf(w, "event: log\ndata: %s\n\n", line)
		fl.Flush()
	}
	job.mu.Lock()
	if job.err != nil {
		fmt.Fprintf(w, "event: error\ndata: %s\n\n", job.err.Error())
	} else {
		fmt.Fprintf(w, "event: done\ndata: %s\n\n", "/download?job="+id)
	}
	job.mu.Unlock()
	fl.Flush()
}

func (s *Server) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("job")
	s.mu.Lock()
	job := s.jobs[id]
	s.mu.Unlock()
	if job == nil || !job.done || job.err != nil {
		http.Error(w, "aún no está listo", 404)
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", job.outName))
	w.Header().Set("Content-Type", "application/pdf")
	http.ServeFile(w, r, job.outPath)
}
