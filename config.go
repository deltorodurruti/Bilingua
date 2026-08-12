package main

import (
	"os"
	"path/filepath"
	"strings"
)

// keyPath is where the DeepL key is remembered
// (Windows: %AppData%\Bilingua\ · macOS: ~/Library/Application Support/Bilingua/).
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
