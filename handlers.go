package main

import (
	"boot.dev/linko/internal/store"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"

	"net/http"
	"net/url"
	"strings"
	"sync"
)

const shortURLLen = len("http://localhost:8080/") + 6

var (
	redirectsMu sync.Mutex
	redirects   []string
)

//go:embed index.html
var indexPage string

func (s *server) handlerIndex(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "handler.index")
	defer span.End()

	w.Header().Set("Content-Type", "text/html")
	io.WriteString(w, indexPage)
}

func (s *server) handlerLogin(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "handler.login")
	defer span.End()

	w.WriteHeader(http.StatusOK)
}

func (s *server) handlerShortenLink(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.shorten_link")
	defer span.End()
	user, ok := ctx.Value(UserContextKey).(string)

	if !ok || user == "" {
		httpError(ctx, w, http.StatusUnauthorized, errors.New("unauthorized"))
		//http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	longURL := r.FormValue("url")
	if longURL == "" {
		httpError(ctx, w, http.StatusBadRequest, errors.New("missing url parameter"))
		//http.Error(w, "missing url parameter", http.StatusBadRequest)
		return
	}
	//s.logger.Info("Shortening URL", "long_url", longURL)
	u, err := url.Parse(longURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		httpError(ctx, w, http.StatusBadRequest, errors.New("invalid URL: must include scheme (http/https) and host"))
		//http.Error(w, "invalid URL: must include scheme (http/https) and host", http.StatusBadRequest)
		return
	}
	//s.logger.Info(fmt.Sprintf("Parsed URL: scheme=%s, host=%s\n", u.Scheme, u.Host))
	if err := checkDestination(ctx, longURL); err != nil {
		httpError(ctx, w, http.StatusBadRequest, errors.New(fmt.Sprintf("invalid target URL: %v", err)))
		//http.Error(w, fmt.Sprintf("invalid target URL: %v", err), http.StatusBadRequest)
		return
	}
	shortCode, err := s.store.Create(ctx, longURL)
	if err != nil {
		httpError(ctx, w, http.StatusInternalServerError, fmt.Errorf("failed to shorten URL: %w", err))
		//http.Error(w, "failed to shorten URL", http.StatusInternalServerError)
		return
	}
	s.logger.Info("Successfully generated short code", "short_code", shortCode, "long_url", longURL)
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusCreated)
	io.WriteString(w, shortCode)
}

func (s *server) handlerRedirect(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.redirect")
	defer span.End()

	longURL, err := s.store.Lookup(ctx, r.PathValue("shortCode"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(ctx, w, http.StatusNotFound, errors.New("not found"))
			//http.Error(w, "not found", http.StatusNotFound)
		} else {
			s.logger.Error("failed to lookup URL", "error", err)
			httpError(ctx, w, http.StatusInternalServerError, fmt.Errorf("internal server error: %w", err))
			//http.Error(w, "internal server error", http.StatusInternalServerError)
		}
		return
	}
	_, _ = bcrypt.GenerateFromPassword([]byte(longURL), bcrypt.DefaultCost)
	if err := checkDestination(ctx, longURL); err != nil {
		httpError(ctx, w, http.StatusBadGateway, errors.New("destination unavailable"))
		//http.Error(w, "destination unavailable", http.StatusBadGateway)
		return
	}

	redirectsMu.Lock()
	redirects = append(redirects, strings.Repeat(longURL, 1024))
	redirectsMu.Unlock()

	http.Redirect(w, r, longURL, http.StatusFound)
}

func (s *server) handlerListURLs(w http.ResponseWriter, r *http.Request) {
	ctx, span := tracer.Start(r.Context(), "handler.list_urls")
	defer span.End()

	codes, err := s.store.List(ctx)
	if err != nil {
		//s.logger.Error("failed to list URLs", "error", err)
		httpError(ctx, w, http.StatusInternalServerError, errors.New("failed to list URLs"))
		//http.Error(w, "failed to list URLs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(codes)
}

func (s *server) handlerStats(w http.ResponseWriter, r *http.Request) {
	_, span := tracer.Start(r.Context(), "handler.stats")
	defer span.End()

	redirectsMu.Lock()
	snapshot := redirects
	redirectsMu.Unlock()

	var bytesSaved int
	for _, u := range snapshot {
		bytesSaved += len(u) - shortURLLen
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]int{
		"redirects":   len(snapshot),
		"bytes_saved": bytesSaved,
	})
}
