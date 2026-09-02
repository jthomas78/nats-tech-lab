package main

import (
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// StaticHost is deliberately not a general web framework. Its complete route
// set is the bounded health endpoint and the asset root.
type StaticHost struct {
	root          string
	allowedOrigin string
	assetHandler  http.Handler
	failures      chan error
}

func NewStaticHost(root, allowedOrigin string) (*StaticHost, error) {
	if allowedOrigin == "" {
		return nil, fmt.Errorf("ASSET_ALLOWED_ORIGIN is required")
	}
	origin, err := url.Parse(allowedOrigin)
	if err != nil || (origin.Scheme != "http" && origin.Scheme != "https") || origin.Host == "" || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
		return nil, fmt.Errorf("ASSET_ALLOWED_ORIGIN must be one named HTTP(S) origin")
	}
	dir, err := os.Open(root)
	if err != nil {
		return nil, fmt.Errorf("open asset root: %w", err)
	}
	info, statErr := dir.Stat()
	closeErr := dir.Close()
	if statErr != nil {
		return nil, fmt.Errorf("inspect asset root: %w", statErr)
	}
	if closeErr != nil {
		return nil, fmt.Errorf("close asset root: %w", closeErr)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("asset root is not a directory")
	}
	h := &StaticHost{root: filepath.Clean(root), allowedOrigin: allowedOrigin, failures: make(chan error, 1)}
	h.assetHandler = http.HandlerFunc(h.serveAsset)
	return h, nil
}

func (h *StaticHost) RoutePatterns() []string { return []string{"/healthz", "/"} }
func (h *StaticHost) Failures() <-chan error  { return h.failures }

func (h *StaticHost) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/healthz" {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
		return
	}
	w.Header().Set("Access-Control-Allow-Origin", h.allowedOrigin)
	w.Header().Set("Vary", "Origin")
	defer func() {
		if recovered := recover(); recovered != nil {
			err := fmt.Errorf("asset handler panic: %v", recovered)
			select {
			case h.failures <- err:
			default:
			}
			http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
		}
	}()
	h.assetHandler.ServeHTTP(w, r)
}

func (h *StaticHost) serveAsset(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	for _, segment := range strings.Split(path, "/") {
		if segment == ".." {
			http.NotFound(w, r)
			return
		}
	}
	rel := strings.TrimPrefix(path, "/")
	candidate := filepath.Join(h.root, filepath.FromSlash(rel))
	contained, err := filepath.Rel(h.root, candidate)
	if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) {
		http.NotFound(w, r)
		return
	}
	info, err := os.Stat(candidate)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if info.IsDir() {
		candidate = filepath.Join(candidate, "index.html")
		info, err = os.Stat(candidate)
		if err != nil || info.IsDir() {
			http.NotFound(w, r)
			return
		}
	}
	file, err := os.Open(candidate)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	if contentType := mime.TypeByExtension(filepath.Ext(candidate)); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), file)
}
