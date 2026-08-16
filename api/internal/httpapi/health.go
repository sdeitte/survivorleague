package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

type healthResponse struct {
	Status string `json:"status"`
	DB     string `json:"db"`
	Error  string `json:"error,omitempty"`
}

func (a *API) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if a.pool == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(healthResponse{
			Status: "error",
			DB:     "error",
			Error:  "DATABASE_URL is not configured",
		})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	if err := a.pool.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(healthResponse{
			Status: "error",
			DB:     "error",
			Error:  err.Error(),
		})
		return
	}

	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(healthResponse{
		Status: "ok",
		DB:     "ok",
	})
}
