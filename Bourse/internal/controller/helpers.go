package controller

import (
	"encoding/json"
	"errors"
	"net/http"

	"bourse/internal/repository"
	"bourse/internal/service"
)

// writeJSON encodes v as a JSON response with the given status.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr maps domain errors to HTTP status codes.
func writeErr(w http.ResponseWriter, err error) {
	var ve service.ValidationError
	var ie service.InvariantError
	switch {
	case errors.As(err, &ve):
		writeJSON(w, http.StatusBadRequest, errBody{ve.Error()})
	case errors.As(err, &ie):
		writeJSON(w, http.StatusUnprocessableEntity, errBody{ie.Error()})
	case errors.Is(err, repository.ErrNotFound):
		writeJSON(w, http.StatusNotFound, errBody{"not found"})
	default:
		writeJSON(w, http.StatusInternalServerError, errBody{err.Error()})
	}
}

type errBody struct {
	Error string `json:"error"`
}

// decode reads a JSON request body into v.
func decode(r *http.Request, v any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(v)
}
