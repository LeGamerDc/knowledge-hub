package service

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/legamerdc/knowledge-hub/internal/server/handlers"
	"github.com/legamerdc/knowledge-hub/pkg/corestore"
)

// KHService implements handlers.ServerInterface using corestore as the backend.
type KHService struct {
	store corestore.Store
}

// New creates a new KHService.
func New(store corestore.Store) *KHService {
	return &KHService{store: store}
}

var _ handlers.ServerInterface = (*KHService)(nil)

// writeJSON encodes v to JSON and writes it with the given HTTP status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, handlers.ErrorResponse{Code: status, Message: message})
}

// isNotFound checks if an error is ErrNotFound.
func isNotFound(err error) bool {
	return errors.Is(err, corestore.ErrNotFound)
}

// decode decodes the JSON request body into v, writing an error and returning false on failure.
func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return false
	}
	return true
}
