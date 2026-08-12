package main

import (
	"os"
	"path/filepath"
	"strings"
)

// keyPath is where the DeepL keys are remembered, one per line
// (Windows: %AppData%\Bilingua\ · macOS: ~/Library/Application Support/Bilingua/).
func keyPath() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		dir = "."
	}
	return filepath.Join(dir, "Bilingua", "deepl_key.txt")
}

// loadKeys returns the stored keys in order: the first is used until its monthly
// quota runs out, then the next one takes over.
func loadKeys() []string {
	b, err := os.ReadFile(keyPath())
	if err != nil {
		return nil
	}
	return splitKeys(string(b))
}

func loadKey() string {
	if k := loadKeys(); len(k) > 0 {
		return k[0]
	}
	return ""
}

// splitKeys accepts keys separated by newlines or commas and drops duplicates.
func splitKeys(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.FieldsFunc(s, func(r rune) bool { return r == '\n' || r == '\r' || r == ',' }) {
		k := strings.TrimSpace(line)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out
}

// addKeys appends keys that are not stored yet, keeping the existing order.
func addKeys(keys []string) []string {
	all := loadKeys()
	seen := map[string]bool{}
	for _, k := range all {
		seen[k] = true
	}
	for _, k := range keys {
		if k != "" && !seen[k] {
			seen[k] = true
			all = append(all, k)
		}
	}
	saveKeys(all)
	return all
}

func saveKeys(keys []string) {
	if len(keys) == 0 {
		return
	}
	p := keyPath()
	_ = os.MkdirAll(filepath.Dir(p), 0700)
	_ = os.WriteFile(p, []byte(strings.Join(keys, "\n")+"\n"), 0600)
}

func saveKey(key string) {
	if k := splitKeys(key); len(k) > 0 {
		addKeys(k)
	}
}
