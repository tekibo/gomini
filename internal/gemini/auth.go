package gemini

import (
	"context"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
)

// CookieStore stores and caches cookie strings and SAPISID values with file-stat invalidation.
type CookieStore struct {
	mu      sync.RWMutex
	cookie  string
	sapisid string
	mtime   time.Time
	size    int64
}

var (
	reSAPISID = regexp.MustCompile(`(?:^|;\s*)SAPISID=([^;\s]+)`)
	reXSRF    = regexp.MustCompile(`"SNlM0e":"([^"]+)"`)
)

// Load reads and parses cookies from the specified file path, returning cookie string and SAPISID.
func (cs *CookieStore) Load(path string) (cookie, sapisid string) {
	if path == "" {
		return "", ""
	}
	info, err := os.Stat(path)
	if err != nil {
		cs.mu.RLock()
		defer cs.mu.RUnlock()
		return cs.cookie, cs.sapisid
	}

	cs.mu.RLock()
	cached := !info.ModTime().After(cs.mtime) && info.Size() == cs.size && cs.cookie != ""
	cs.mu.RUnlock()

	if cached {
		cs.mu.RLock()
		defer cs.mu.RUnlock()
		return cs.cookie, cs.sapisid
	}

	data, err := os.ReadFile(path)
	if err != nil {
		cs.mu.RLock()
		defer cs.mu.RUnlock()
		return cs.cookie, cs.sapisid
	}

	content := strings.TrimSpace(string(data))
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
	} else if strings.Contains(content, "# Netscape HTTP Cookie File") || strings.HasPrefix(content, "#HttpOnly_") || strings.Contains(content, "\t") {
		cookieStr = ParseNetscapeCookies(content)
		pairs := parseCookiePairs(cookieStr)
		sapisidVal = pairs["SAPISID"]
	} else {
		cookieStr = content
		pairs := parseCookiePairs(content)
		sapisidVal = pairs["SAPISID"]
	}

	if sapisidVal == "" && cookieStr != "" {
		if m := reSAPISID.FindStringSubmatch(cookieStr); len(m) > 1 {
			sapisidVal = m[1]
		}
	}

	cs.mu.Lock()
	cs.cookie = cookieStr
	cs.sapisid = sapisidVal
	cs.mtime = info.ModTime()
	cs.size = info.Size()
	cs.mu.Unlock()

	return cookieStr, sapisidVal
}

// Set updates the in-memory cookie string and SAPISID directly.
func (cs *CookieStore) Set(cookieStr, sapisid string) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.cookie = cookieStr
	cs.sapisid = sapisid
	cs.mtime = time.Now()
}

// ParseNetscapeCookies parses Netscape cookies.txt format and extracts Google cookies into a single header string.
func ParseNetscapeCookies(raw string) string {
	pairs := make(map[string]string)
	lines := strings.Split(raw, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#HttpOnly_") {
			line = strings.TrimPrefix(line, "#HttpOnly_")
		} else if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Split(line, "\t")
		if len(parts) >= 7 {
			domain := strings.ToLower(strings.TrimPrefix(parts[0], "."))
			if domain == "google.com" || strings.HasSuffix(domain, ".google.com") {
				pairs[parts[5]] = parts[6]
			}
		}
	}
	var pairsList []string
	for k, v := range pairs {
		pairsList = append(pairsList, fmt.Sprintf("%s=%s", k, v))
	}
	return strings.Join(pairsList, "; ")
}

func parseCookiePairs(s string) map[string]string {
	pairs := make(map[string]string)
	for item := range strings.SplitSeq(s, ";") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if idx := strings.Index(item, "="); idx >= 0 {
			k := strings.TrimSpace(item[:idx])
			v := strings.TrimSpace(item[idx+1:])
			if k != "" {
				pairs[k] = v
			}
		}
	}
	return pairs
}

// MakeSapisidHash creates an authorization header value for Google SAPISID authentication.
func MakeSapisidHash(sapisid string) string {
	ts := time.Now().Unix()
	h := sha1.New()
	_, _ = fmt.Fprintf(h, "%d %s https://gemini.google.com", ts, sapisid)
	return fmt.Sprintf("SAPISIDHASH %d_%x", ts, h.Sum(nil))
}

// RefreshBLAndXSRF scrapes gemini.google.com/app using cookies to get the current gemini_bl and xsrf_token (SNlM0e).
func RefreshBLAndXSRF(ctx context.Context, client *http.Client, cookies *CookieStore, cookiePath string, authUser *string) (newBL string, newXSRF string, err error) {
	appURL := "https://gemini.google.com/app"
	if authUser != nil && *authUser != "" {
		appURL = fmt.Sprintf("https://gemini.google.com/u/%s/app", *authUser)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", appURL, nil)
	if err != nil {
		return "", "", fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	if cookies != nil && cookiePath != "" {
		cookieStr, sapisid := cookies.Load(cookiePath)
		if cookieStr != "" {
			req.Header.Set("Cookie", cookieStr)
		}
		if sapisid != "" {
			req.Header.Set("Authorization", MakeSapisidHash(sapisid))
		}
	}
	if authUser != nil && *authUser != "" {
		req.Header.Set("X-Goog-AuthUser", *authUser)
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("refresh request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("read refresh body: %w", err)
	}
	body := string(bodyBytes)

	if m := reXSRF.FindStringSubmatch(body); len(m) > 1 {
		newXSRF = m[1]
	}
	if m := reBL.FindStringSubmatch(body); len(m) > 1 {
		newBL = m[1]
	}

	return newBL, newXSRF, nil
}
