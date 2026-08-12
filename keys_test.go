package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

func TestSplitKeysDropsDuplicatesAndBlanks(t *testing.T) {
	got := splitKeys(" a:fx , b:fx\n\n a:fx \n c \n")
	want := []string{"a:fx", "b:fx", "c"}
	if len(got) != len(want) {
		t.Fatalf("esperaba %v, obtuvo %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("esperaba %v, obtuvo %v", want, got)
		}
	}
}

// A second --key must add to the stored list, not replace it.
func TestAddKeysAppends(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir) // Linux
	t.Setenv("HOME", dir)            // macOS resolves the config dir from HOME
	if err := os.MkdirAll(dir+"/Library/Application Support", 0o700); err != nil {
		t.Fatal(err)
	}

	addKeys([]string{"primera:fx"})
	addKeys([]string{"segunda:fx"})
	addKeys([]string{"primera:fx"}) // repetida: no debe duplicarse

	got := loadKeys()
	if len(got) != 2 || got[0] != "primera:fx" || got[1] != "segunda:fx" {
		t.Fatalf("esperaba las dos claves en orden, obtuvo %v", got)
	}
	if loadKey() != "primera:fx" {
		t.Errorf("la primera clave debe ser la que se usa por defecto")
	}
	if fi, err := os.Stat(keyPath()); err != nil {
		t.Fatal(err)
	} else if fi.Mode().Perm() != 0o600 {
		t.Errorf("el archivo de claves debe ser 0600, es %v", fi.Mode().Perm())
	}
}

// With a free key among them the parts must fit the smaller limit, whichever
// key ends up being used.
func TestDocSizeLimitTakesTheSmallest(t *testing.T) {
	if got := DocSizeLimit("pro-una\npro-dos"); got != proDocLimit {
		t.Errorf("solo claves Pro: esperaba el límite Pro, obtuvo %d", got)
	}
	if got := DocSizeLimit("pro-una\ngratis:fx"); got != freeDocLimit {
		t.Errorf("mezcla Pro y Free: debe mandar el límite Free, obtuvo %d", got)
	}
}

func TestNextKeyStopsAtTheLast(t *testing.T) {
	d := NewDeepLKeys([]string{"a:fx", "b:fx"})
	if d.key() != "a:fx" {
		t.Fatalf("debía empezar por la primera")
	}
	if !d.nextKey("agotada") || d.key() != "b:fx" {
		t.Fatalf("debía saltar a la segunda")
	}
	if d.nextKey("agotada") {
		t.Fatalf("no debía haber una tercera")
	}
}

// The first key is out of quota (456); the book must carry on with the second.
func TestRotatesToNextKeyOnQuotaExceeded(t *testing.T) {
	var tried []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := strings.TrimPrefix(r.Header.Get("Authorization"), "DeepL-Auth-Key ")
		tried = append(tried, auth)
		if auth == "agotada:fx" {
			w.WriteHeader(456)
			_, _ = w.Write([]byte(`{"message":"Quota exceeded"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"translations": []map[string]string{{"text": "hola mundo"}},
		})
	}))
	defer srv.Close()

	d := NewDeepLKeys([]string{"agotada:fx", "buena:fx"})
	var notices []string
	d.log = func(s string) { notices = append(notices, s) }
	d.testEndpoint = srv.URL

	out, err := d.Translate([]string{"hello world"}, "EN", "ES")
	if err != nil {
		t.Fatalf("debía continuar con la segunda clave, dio: %v", err)
	}
	if len(out) != 1 || out[0] != "hola mundo" {
		t.Fatalf("traducción inesperada: %v", out)
	}
	if len(tried) != 2 || tried[0] != "agotada:fx" || tried[1] != "buena:fx" {
		t.Fatalf("debía probar la primera y luego la segunda: %v", tried)
	}
	if len(notices) == 0 || !strings.Contains(notices[0], "clave 2") {
		t.Fatalf("debía avisar del cambio de clave: %v", notices)
	}
}
