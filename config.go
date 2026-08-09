package main

import (
	"os"
	"path/filepath"
	"strings"
)

// keyPath: archivo local donde se recuerda la clave de DeepL
// (Windows: %AppData%\Bilingua\deepl_key.txt · Mac: ~/Library/Application Support/…).
func keyPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "Bilingua", "deepl_key.txt")
}

func loadKey() string {
	b, err := os.ReadFile(keyPath())
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func saveKey(key string) {
	key = strings.TrimSpace(key)
	if key == "" {
		return
	}
	p := keyPath()
	_ = os.MkdirAll(filepath.Dir(p), 0700)
	_ = os.WriteFile(p, []byte(key), 0600)
}
