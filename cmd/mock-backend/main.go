package main

import (
	"encoding/json"
	"log"
	"net/http"
	"time"
)

func main() {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/fast", handleFast)
	mux.HandleFunc("/api/slow", handleSlow)
	mux.HandleFunc("/api/expensive", handleExpensive)
	mux.HandleFunc("/health", handleHealth)

	log.Println("mock backend listening on :8081")
	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatal(err)
	}
}

// GET /api/fast — simulates a lightweight read endpoint
func handleFast(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"endpoint": "fast",
		"message":  "ok",
	})
}

// GET /api/slow — simulates a DB-heavy or IO-bound endpoint
func handleSlow(w http.ResponseWriter, r *http.Request) {
	time.Sleep(200 * time.Millisecond)
	writeJSON(w, http.StatusOK, map[string]string{
		"endpoint": "slow",
		"message":  "ok",
	})
}

// POST /api/expensive — simulates a write or compute-heavy endpoint
// This endpoint will be assigned a higher token cost in rate limit policy.
func handleExpensive(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"endpoint": "expensive",
		"message":  "ok",
	})
}

// GET /health — used by Docker Compose and the gateway for liveness checks
func handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(body)
}
