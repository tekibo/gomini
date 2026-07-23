package gemini

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

type CookieStore struct {
	mu      sync.RWMutex
	cookie  string
	sapisid string
	mtime   time.Time
}

func (cs *CookieStore) Load(path string) (cookie, sapisid string) {
	if path == "" {
		return "", ""
	}
	info, err := os.Stat(path)
	if err != nil {
		return cs.cookie, cs.sapisid
	}

	cs.mu.RLock()
	cached := !info.ModTime().After(cs.mtime) && cs.cookie != ""
	cs.mu.RUnlock()

	if cached {
		return cs.cookie, cs.sapisid
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return cs.cookie, cs.sapisid
	}

	content := string(data)
	var cookieStr, sapisidVal string

	if len(content) > 0 && content[0] == '{' {
		var jsonData struct {
			Cookie  string `json:"cookie"`
			Sapisid string `json:"sapisid"`
		}
		if err := json.Unmarshal([]byte(content), &jsonData); err == nil {
			cookieStr = jsonData.Cookie
			sapisidVal = jsonData.Sapisid
		}
	} else {
		cookieStr = content
		pairs := parseCookiePairs(content)
		sapisidVal = pairs["SAPISID"]
	}

	cs.mu.Lock()
	cs.cookie = cookieStr
	cs.sapisid = sapisidVal
	cs.mtime = info.ModTime()
	cs.mu.Unlock()

	return cookieStr, sapisidVal
}

func parseCookiePairs(s string) map[string]string {
	pairs := make(map[string]string)
	i := 0
	for i < len(s) {
		for i < len(s) && s[i] == ';' || s[i] == ' ' {
			i++
		}
		if i >= len(s) {
			break
		}
		start := i
		for i < len(s) && s[i] != '=' {
			i++
		}
		if i >= len(s) {
			break
		}
		key := s[start:i]
		i++ // skip =
		start = i
		for i < len(s) && s[i] != ';' {
			i++
		}
		val := s[start:i]
		if key != "" {
			pairs[key] = val
		}
		i++ // skip ;
	}
	return pairs
}

func MakeSapisidHash(sapisid string) string {
	ts := time.Now().Unix()
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "%d %s https://gemini.google.com", ts, sapisid)
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, h.Sum(nil))
}
