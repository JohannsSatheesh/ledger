// Package api exposes the ledger over HTTP.
//
// Contract (keep README in sync):
//
//	POST /accounts            {"id":"alice","opening_balance":1000}
//	    201 created | 400 bad input | 409 account exists
//	POST /transfers           {"from":"alice","to":"bob","amount":250}
//	                          header: Idempotency-Key: <string>   (required)
//	    201 created | 200 replay of an earlier identical request
//	    400 bad input / missing key | 404 unknown account
//	    409 key reused with a different request | 422 insufficient funds
//	GET  /accounts/{id}       -> {"id":"alice","balance":750}
//	    200 | 404
//
// Errors are returned as {"error":"message"}.
package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/JohannsSatheesh/ledger/internal/ledger"
)

// Server wires HTTP routes to a Ledger.
type Server struct {
	ledger *ledger.Ledger
	mux    *http.ServeMux
}

// NewServer returns an http.Handler serving the API.
func NewServer(l *ledger.Ledger) http.Handler {
	s := &Server{ledger: l, mux: http.NewServeMux()}

	// Go 1.22+ patterns: method and path wildcards are built in.
	s.mux.HandleFunc("POST /accounts", s.createAccount)
	s.mux.HandleFunc("POST /transfers", s.createTransfer)
	s.mux.HandleFunc("GET /accounts/{id}", s.getAccount)
	return s.mux
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request) {
	var in struct {
		ID      string `json:"id"`
		Opening int64  `json:"opening_balance"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.ID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	if err := s.ledger.CreateAccount(ledger.AccountID(in.ID), ledger.Money(in.Opening)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": in.ID, "balance": in.Opening})
}

func (s *Server) createTransfer(w http.ResponseWriter, r *http.Request) {
	var in struct {
		From   string `json:"from"`
		To     string `json:"to"`
		Amount int64  `json:"amount"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}

	tr, replayed, err := s.ledger.Transfer(ledger.TransferRequest{
		From:           ledger.AccountID(in.From),
		To:             ledger.AccountID(in.To),
		Amount:         ledger.Money(in.Amount),
		IdempotencyKey: r.Header.Get("Idempotency-Key"),
	})
	if err != nil {
		writeError(w, err)
		return
	}

	status := http.StatusCreated
	if replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, tr)
}

func (s *Server) getAccount(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	balance, err := s.ledger.Balance(ledger.AccountID(id))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "balance": balance})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, ledger.ErrAccountNotFound):
		status = http.StatusNotFound
	case errors.Is(err, ledger.ErrAccountExists), errors.Is(err, ledger.ErrIdempotencyConflict):
		status = http.StatusConflict
	case errors.Is(err, ledger.ErrInsufficientFunds):
		status = http.StatusUnprocessableEntity
	case errors.Is(err, ledger.ErrInvalidAmount), errors.Is(err, ledger.ErrInvalidOpening), errors.Is(err, ledger.ErrSameAccount), errors.Is(err, ledger.ErrMissingKey):
		status = http.StatusBadRequest
	}
	writeJSON(w, status, map[string]string{"error": err.Error()})
}
