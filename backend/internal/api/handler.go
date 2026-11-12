package api

import (
	"encoding/json"
	"log/slog"
	"net/http"
)

type healthResponse struct {
	Status  string `json:"status"`
	Service string `json:"service"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorDetail `json:"error"`
}

func NewHandler(documents *Documents, connections *ModelConnections) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/health", health)
	if documents != nil {
		documents.register(mux)
	}
	if connections != nil {
		connections.register(mux)
	}
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, r, http.StatusNotFound, errorResponse{
			Error: errorDetail{Code: "not_found", Message: "Endpoint not found."},
		})
	})
	return mux
}

func health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeJSON(w, r, http.StatusMethodNotAllowed, errorResponse{
			Error: errorDetail{Code: "method_not_allowed", Message: "Use GET or HEAD for this endpoint."},
		})
		return
	}
	writeJSON(w, r, http.StatusOK, healthResponse{Status: "ok", Service: "eduforge"})
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	if err := json.NewEncoder(w).Encode(value); err != nil {
		slog.ErrorContext(r.Context(), "write JSON response", "error", err)
	}
}
