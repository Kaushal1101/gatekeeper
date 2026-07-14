package main

import (
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"

	"github.com/kaushaljayapragash/gatekeeper/internal/config"
	"github.com/kaushaljayapragash/gatekeeper/internal/middleware"
	"github.com/kaushaljayapragash/gatekeeper/internal/policy"
	gatewayredis "github.com/kaushaljayapragash/gatekeeper/internal/redis"
)

func main() {
	cfgPath := os.Getenv("CONFIG_PATH")
	if cfgPath == "" {
		cfgPath = "config/config.yaml"
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	redisClient := gatewayredis.NewClient()

	matcher, err := policy.New(cfg, redisClient)
	if err != nil {
		log.Fatalf("build policy matcher: %v", err)
	}

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
	mux.Handle("/", middleware.RateLimit(matcher)(proxy))

	log.Printf("gateway listening on :8080 → %s", backendAddr)
	if err := http.ListenAndServe(":8080", loggingMiddleware(mux)); err != nil {
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
