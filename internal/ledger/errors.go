package ledger

import "errors"

var (
	ErrNotImplemented      = errors.New("not implemented")
	ErrAccountNotFound     = errors.New("account not found")
	ErrAccountExists       = errors.New("account already exists")
	ErrInvalidAmount       = errors.New("amount must be a positive number of pence")
	ErrInvalidOpening      = errors.New("opening balance cannot be negative")
	ErrSameAccount         = errors.New("cannot transfer to the same account")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrMissingKey          = errors.New("idempotency key is required")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
)
