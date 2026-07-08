package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
)

func main() {
	backendAddr := os.Getenv("BACKEND_URL")
	if backendAddr == "" {
		backendAddr = "http://localhost:8081"
	}

	backendURL, err := url.Parse(backendAddr)
	if err != nil {
		log.Fatalf("invalid backend URL %q: %v", backendAddr, err)
	}

	proxy := httputil.NewSingleHostReverseProxy(backendURL)

	mux := http.NewServeMux()
	mux.HandleFunc("/health", handleHealth)
	mux.Handle("/", loggingMiddleware(proxy))

	log.Printf("gateway listening on :8080 → %s", backendAddr)
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatal(err)
	}
}

func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(`{"status":"ok"}`))
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}
