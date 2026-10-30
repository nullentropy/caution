package terminal

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// the native terminal talks to one server, so domain and path never need
// matching. persistent cookies go to disk so a restart stays signed in,
// session cookies live as long as the process
type cookieJar struct {
	mu     sync.Mutex
	path   string // "" keeps everything in memory
	byName map[string]*http.Cookie
}

type savedCookie struct {
	Name, Value string
	Expires     time.Time
}

// one file per app and server under the user config dir
func openCookieJar(key string) *cookieJar {
	j := &cookieJar{byName: map[string]*http.Cookie{}}
	dir, err := os.UserConfigDir()
	if err != nil || key == "" {
		return j
	}
	j.path = filepath.Join(dir, "caution", "cookies", key+".json")
	data, err := os.ReadFile(j.path)
	if err != nil {
		return j
	}
	var saved []savedCookie
	if json.Unmarshal(data, &saved) != nil {
		return j
	}
	now := time.Now()
	for _, c := range saved {
		if c.Expires.After(now) {
			j.byName[c.Name] = &http.Cookie{Name: c.Name, Value: c.Value, Expires: c.Expires}
		}
	}
	return j
}

func (j *cookieJar) set(raw string) {
	c, err := http.ParseSetCookie(raw)
	if err != nil {
		log.Printf("caution: bad cookie from server: %v", err)
		return
	}
	j.SetCookies(nil, []*http.Cookie{c})
}

func (j *cookieJar) SetCookies(_ *url.URL, cs []*http.Cookie) {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	for _, c := range cs {
		// Max-Age wins over Expires, as in a browser
		expires := c.Expires
		switch {
		case c.MaxAge > 0:
			expires = now.Add(time.Duration(c.MaxAge) * time.Second)
		case c.MaxAge < 0:
			expires = now
		}
		if !expires.IsZero() && !expires.After(now) {
			delete(j.byName, c.Name)
			continue
		}
		j.byName[c.Name] = &http.Cookie{Name: c.Name, Value: c.Value, Expires: expires}
	}
	j.save()
}

func (j *cookieJar) Cookies(_ *url.URL) []*http.Cookie {
	j.mu.Lock()
	defer j.mu.Unlock()
	now := time.Now()
	var out []*http.Cookie
	for _, c := range j.byName {
		if c.Expires.IsZero() || c.Expires.After(now) {
			out = append(out, &http.Cookie{Name: c.Name, Value: c.Value})
		}
	}
	return out
}

// called with mu held
func (j *cookieJar) save() {
	if j.path == "" {
		return
	}
	var saved []savedCookie
	for _, c := range j.byName {
		if !c.Expires.IsZero() {
			saved = append(saved, savedCookie{c.Name, c.Value, c.Expires})
		}
	}
	data, _ := json.Marshal(saved)
	if err := os.MkdirAll(filepath.Dir(j.path), 0o700); err != nil {
		log.Printf("caution: cookies not saved: %v", err)
		return
	}
	if err := os.WriteFile(j.path, data, 0o600); err != nil {
		log.Printf("caution: cookies not saved: %v", err)
	}
}
