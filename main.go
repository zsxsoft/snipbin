package main

import (
	"crypto/rand"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"runtime"
	"sync"
	"time"

	"github.com/gabriel-vasile/mimetype"
)

//go:embed all:web
var webFS embed.FS

const (
	idAlphabet      = "23456789abcdefghjkmnpqrstuvwxyz"
	idLength        = 3
	defaultTTL      = 1 * time.Hour
	maxTTL          = 24 * time.Hour
	minTTL          = 1 * time.Minute
	maxFileSize     = 100 << 20 // 100MB
	shmThreshold    = 1 << 20   // 1MB — smaller files go to memory if available
	cleanupInterval = 1 * time.Minute
)

var (
	shmBaseDir = "/dev/shm/snipbin"
	tmpBaseDir = filepath.Join(os.TempDir(), "snipbin")
)

var (
	store       sync.Map // id → *Metadata
	baseURL     string
	shmWritable bool
)

// ── Types ──────────────────────────────────────────────────────────────

type Metadata struct {
	ID          string    `json:"id"`
	Filename    string    `json:"filename,omitempty"`
	ContentType string    `json:"contentType"`
	Size        int64     `json:"size"`
	CreatedAt   time.Time `json:"createdAt"`
	ExpiresAt   time.Time `json:"expiresAt"`
	DataPath    string    `json:"dataPath"`
}

type CreateResponse struct {
	ID        string    `json:"id"`
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type RecentItem struct {
	ID          string `json:"id"`
	Filename    string `json:"filename,omitempty"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	CreatedAt   string `json:"createdAt"`
	ExpiresAt   string `json:"expiresAt"`
}

// ── ID generation (nanoid-style) ──────────────────────────────────────

func generateID() string {
	b := make([]byte, idLength)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("rand.Read: %v", err))
	}
	for i := range b {
		b[i] = idAlphabet[b[i]%byte(len(idAlphabet))]
	}
	return string(b)
}

// ── TTL parsing ────────────────────────────────────────────────────────

func parseTTL(r *http.Request) time.Duration {
	s := r.Header.Get("X-TTL")
	if s == "" {
		s = r.URL.Query().Get("ttl")
	}
	if s == "" {
		return defaultTTL
	}
	return clampTTL(parseDuration(s))
}

func parseFormTTL(s string) time.Duration {
	if s == "" {
		return defaultTTL
	}
	return clampTTL(parseDuration(s))
}

func parseDuration(s string) time.Duration {
	if d, err := time.ParseDuration(s); err == nil {
		return d
	}
	var sec int
	if _, err := fmt.Sscanf(s, "%d", &sec); err == nil && sec > 0 {
		return time.Duration(sec) * time.Second
	}
	return defaultTTL
}

func clampTTL(d time.Duration) time.Duration {
	if d < minTTL {
		return minTTL
	}
	if d > maxTTL {
		return maxTTL
	}
	return d
}

// ── Storage helpers ────────────────────────────────────────────────────

func pickDir(size int64) string {
	if size < int64(shmThreshold) && shmWritable {
		return shmBaseDir
	}
	return tmpBaseDir
}

func saveEntry(id string, data []byte, filename string, ttl time.Duration) (*Metadata, error) {
	dir := pickDir(int64(len(data)))
	if err := os.MkdirAll(dir, 0777); err != nil {
		return nil, fmt.Errorf("mkdir %s: %w", dir, err)
	}

	mtype := mimetype.Detect(data)
	ct := mtype.String()
	if ct == "" {
		ct = "application/octet-stream"
	}

	now := time.Now()
	meta := &Metadata{
		ID:          id,
		Filename:    filename,
		ContentType: ct,
		Size:        int64(len(data)),
		CreatedAt:   now,
		ExpiresAt:   now.Add(ttl),
		DataPath:    filepath.Join(dir, id+".data"),
	}

	if err := os.WriteFile(meta.DataPath, data, 0600); err != nil {
		return nil, fmt.Errorf("write data: %w", err)
	}
	metaJSON, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, id+".meta"), metaJSON, 0600); err != nil {
		os.Remove(meta.DataPath)
		return nil, fmt.Errorf("write meta: %w", err)
	}

	store.Store(id, meta)
	return meta, nil
}

func deleteEntry(id string) {
	v, ok := store.LoadAndDelete(id)
	if !ok {
		return
	}
	m := v.(*Metadata)
	dir := filepath.Dir(m.DataPath)
	os.Remove(m.DataPath)
	os.Remove(filepath.Join(dir, id+".meta"))
}

// ── Startup scan & cleanup ────────────────────────────────────────────

func scanExisting() {
	for _, dir := range []string{shmBaseDir, tmpBaseDir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".meta") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			var m Metadata
			if err := json.Unmarshal(raw, &m); err != nil {
				continue
			}
			if time.Now().After(m.ExpiresAt) {
				dir2 := filepath.Dir(m.DataPath)
				os.Remove(m.DataPath)
				os.Remove(filepath.Join(dir2, m.ID+".meta"))
				continue
			}
			store.Store(m.ID, &m)
		}
	}
}

func cleanupLoop() {
	t := time.NewTicker(cleanupInterval)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		store.Range(func(k, v any) bool {
			m := v.(*Metadata)
			if now.After(m.ExpiresAt) {
				dir := filepath.Dir(m.DataPath)
				os.Remove(m.DataPath)
				os.Remove(filepath.Join(dir, m.ID+".meta"))
				store.Delete(k)
				log.Printf("[CLEANUP] expired %s", m.ID)
			}
			return true
		})
	}
}

// ── URL builder ────────────────────────────────────────────────────────

func makeShareURL(id string, r *http.Request) string {
	if baseURL != "" {
		return fmt.Sprintf("%s/s/%s", strings.TrimRight(baseURL, "/"), id)
	}
	scheme := "http"
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/s/%s", scheme, r.Host, id)
}

// ── Handlers ───────────────────────────────────────────────────────────

func handleHealthz(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// PUT / — curl upload, body is the content
func handlePut(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if len(data) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	ttl := parseTTL(r)

	// Optional filename: X-Filename > Content-Disposition > URL path
	filename := ""
	if xf := r.Header.Get("X-Filename"); xf != "" {
		filename = xf
	} else if cd := r.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			filename = params["filename"]
		}
	}
	if filename == "" {
		if p := strings.TrimPrefix(r.URL.Path, "/"); p != "" {
			filename = filepath.Base(p)
		}
	}

	id := generateID()
	meta, err := saveEntry(id, data, filename, ttl)
	if err != nil {
		log.Printf("[PUT] save error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	shareURL := makeShareURL(id, r)
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateResponse{
			ID:        id,
			URL:       shareURL,
			ExpiresAt: meta.ExpiresAt,
		})
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(shareURL))
	}
}

// POST / — web form upload
func handlePost(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxFileSize+1<<20)
	if err := r.ParseMultipartForm(maxFileSize); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	ttl := parseFormTTL(r.FormValue("ttl"))

	var data []byte
	var filename string

	if file, header, err := r.FormFile("file"); err == nil {
		defer file.Close()
		data, err = io.ReadAll(file)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		filename = header.Filename
	}

	if data == nil {
		if text := r.FormValue("text"); text != "" {
			data = []byte(text)
		}
	}

	if data == nil {
		http.Error(w, "no content", http.StatusBadRequest)
		return
	}

	id := generateID()
	meta, err := saveEntry(id, data, filename, ttl)
	if err != nil {
		log.Printf("[POST] save error: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	shareURL := makeShareURL(id, r)
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(CreateResponse{
			ID:        id,
			URL:       shareURL,
			ExpiresAt: meta.ExpiresAt,
		})
	} else {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte(shareURL))
	}
}

// DELETE /s/{id} — delete shared content
func handleDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/s/")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	v, ok := store.Load(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	deleteEntry(v.(*Metadata).ID)
	w.WriteHeader(http.StatusNoContent)
}

// GET /s/{id} — download shared content
const strictCSP = "default-src 'none'; script-src 'none'; style-src 'none'; img-src 'none'; " +
	"connect-src 'none'; font-src 'none'; object-src 'none'; media-src 'none'; " +
	"frame-src 'none'; child-src 'none'; form-action 'none'; base-uri 'none'"

func handleGet(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/s/")
	if id == "" {
		http.NotFound(w, r)
		return
	}

	v, ok := store.Load(id)
	if !ok {
		http.NotFound(w, r)
		return
	}
	meta := v.(*Metadata)

	if time.Now().After(meta.ExpiresAt) {
		deleteEntry(id)
		http.NotFound(w, r)
		return
	}

	f, err := os.Open(meta.DataPath)
	if err != nil {
		deleteEntry(id)
		http.Error(w, "file gone", http.StatusGone)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", meta.ContentType)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Security-Policy", strictCSP)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", meta.Size))

	// By default serve inline; only force download for types browsers can't render
	// CSP already blocks all scripts, so inline is safe
	needsDownload := strings.HasPrefix(meta.ContentType, "application/octet-stream") ||
		strings.HasPrefix(meta.ContentType, "application/zip") ||
		strings.HasPrefix(meta.ContentType, "application/x-") ||
		strings.HasPrefix(meta.ContentType, "application/gzip") ||
		strings.HasPrefix(meta.ContentType, "application/x-tar") ||
		strings.HasPrefix(meta.ContentType, "application/vnd.") ||
		strings.HasSuffix(meta.ContentType, "+zip")

	if needsDownload {
		fn := meta.Filename
		if fn == "" {
			fn = meta.ID
		}
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fn))
	} else {
		w.Header().Set("Content-Disposition", "inline")
	}

	io.Copy(w, f)
}

// GET /api/recent — list recent shares
func handleRecent(w http.ResponseWriter, _ *http.Request) {
	var items []RecentItem
	now := time.Now()
	store.Range(func(_, v any) bool {
		m := v.(*Metadata)
		if now.After(m.ExpiresAt) {
			return true
		}
		items = append(items, RecentItem{
			ID:          m.ID,
			Filename:    m.Filename,
			ContentType: m.ContentType,
			Size:        m.Size,
			CreatedAt:   m.CreatedAt.Format(time.RFC3339),
			ExpiresAt:   m.ExpiresAt.Format(time.RFC3339),
		})
		return true
	})
	sort.Slice(items, func(i, j int) bool {
		return items[i].CreatedAt > items[j].CreatedAt
	})
	if len(items) > 50 {
		items = items[:50]
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(items)
}

// GET /* — serve embedded web UI
func handleUI(w http.ResponseWriter, r *http.Request) {
	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		http.Error(w, "ui unavailable", http.StatusServiceUnavailable)
		return
	}

	// SPA fallback: serve index.html for unknown paths
	path := strings.TrimPrefix(r.URL.Path, "/")
	if path == "" {
		path = "index.html"
	}
	if _, err := fs.Stat(sub, path); err != nil {
		path = "index.html"
		r2 := new(http.Request)
		*r2 = *r
		r2.URL = &url.URL{Path: "/index.html", RawQuery: r.URL.RawQuery}
		http.FileServer(http.FS(sub)).ServeHTTP(w, r2)
		return
	}

	http.FileServer(http.FS(sub)).ServeHTTP(w, r)
}

// ── Main ───────────────────────────────────────────────────────────────

func main() {
	addr := os.Getenv("LISTEN_ADDR")
	if addr == "" {
		addr = ":8080"
	}
	baseURL = os.Getenv("BASE_URL")

	// Probe /dev/shm writability (Linux only)
	if runtime.GOOS == "linux" {
		if err := os.MkdirAll(shmBaseDir, 0777); err == nil {
			probe := filepath.Join(shmBaseDir, ".probe")
			if f, err := os.Create(probe); err == nil {
				f.Close()
				os.Remove(probe)
				shmWritable = true
			}
		}
	}
	os.MkdirAll(tmpBaseDir, 0777)

	scanExisting()
	go cleanupLoop()

	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", handleHealthz)
	mux.HandleFunc("/s/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			handleDelete(w, r)
		} else {
			handleGet(w, r)
		}
	})
	mux.HandleFunc("/api/recent", handleRecent)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			handlePut(w, r)
		case http.MethodPost:
			handlePost(w, r)
		default:
			handleUI(w, r)
		}
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	log.Printf("snipbin listening on %s (shm=%v)", addr, shmWritable)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("listen: %v", err)
	}
}
