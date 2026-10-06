// Package ledger holds the core money-moving logic.
//
// Rules to enforce (see TASKS.md for the build order):
//   - Money is an int64 number of pence. Never use floats for money.
//   - A transfer debits one account and credits another by the same amount,
//     so the total across all accounts never changes (see Total).
//   - Balances never go negative.
//   - A transfer carrying an idempotency key that has been seen before must
//     NOT move money again: replay returns the original result.
//   - The same key with a different request is an error, not a replay.
//   - Everything must be safe to call from many goroutines at once.
package ledger

import (
	"fmt"
	"sync"
)

// AccountID identifies an account.
type AccountID string

// Money is an amount in pence.
type Money int64

// Account is a single balance.
type Account struct {
	ID      AccountID
	Balance Money
}

// TransferRequest is what a caller asks for.
type TransferRequest struct {
	From           AccountID
	To             AccountID
	Amount         Money
	IdempotencyKey string
}

// Transfer is a completed transfer.
type Transfer struct {
	ID     string
	From   AccountID
	To     AccountID
	Amount Money
}

type seenTransfer struct {
	req TransferRequest
	t   Transfer
}

// Ledger is an in-memory ledger. Create one with New.
type Ledger struct {
	mu       sync.Mutex
	accounts map[AccountID]*Account
	seen     map[string]seenTransfer
	n        int
}

// New returns an empty ledger.
func New() *Ledger {
	return &Ledger{
		accounts: make(map[AccountID]*Account),
		seen:     make(map[string]seenTransfer),
	}
}

// CreateAccount opens an account with an opening balance.
// Errors: ErrAccountExists, ErrInvalidOpening.
func (l *Ledger) CreateAccount(id AccountID, opening Money) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if opening < 0 {
		return ErrInvalidOpening
	}
	if _, exists := l.accounts[id]; exists {
		return ErrAccountExists
	}

	l.accounts[id] = &Account{ID: id, Balance: opening}
	return nil
}

// Balance returns the current balance of an account.
// Errors: ErrAccountNotFound.
func (l *Ledger) Balance(id AccountID) (Money, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	account, exists := l.accounts[id]
	if !exists {
		return 0, ErrAccountNotFound
	}
	return account.Balance, nil
}

// Total returns the sum of all balances. It must be unchanged by any transfer,
// which makes it the invariant the concurrency tests check.
func (l *Ledger) Total() Money {
	l.mu.Lock()
	defer l.mu.Unlock()

	var total Money
	for _, account := range l.accounts {
		total += account.Balance
	}
	return total
}

// Transfer moves money between two accounts.
//
// replayed is true when the idempotency key was already used with an identical
// request, in which case no money moves and the original Transfer is returned.
//
// Errors: ErrMissingKey, ErrInvalidAmount, ErrSameAccount, ErrAccountNotFound,
// ErrInsufficientFunds, ErrIdempotencyConflict.
//
// Think about the order of your checks: which should win if a replayed request
// would now fail with insufficient funds?
func (l *Ledger) Transfer(req TransferRequest) (t Transfer, replayed bool, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if req.IdempotencyKey == "" {
		return Transfer{}, false, ErrMissingKey
	}
	if prev, ok := l.seen[req.IdempotencyKey]; ok {
		if prev.req == req {
			return prev.t, true, nil
		}
		return Transfer{}, false, ErrIdempotencyConflict
	}
	if req.Amount <= 0 {
		return Transfer{}, false, ErrInvalidAmount
	}
	if req.From == req.To {
		return Transfer{}, false, ErrSameAccount
	}

	from, ok := l.accounts[req.From]
	if !ok {
		return Transfer{}, false, ErrAccountNotFound
	}
	to, ok := l.accounts[req.To]
	if !ok {
		return Transfer{}, false, ErrAccountNotFound
	}
	if from.Balance < req.Amount {
		return Transfer{}, false, ErrInsufficientFunds
	}

	from.Balance -= req.Amount
	to.Balance += req.Amount

	l.n++
	result := Transfer{ID: fmt.Sprintf("t-%d", l.n), From: req.From, To: req.To, Amount: req.Amount}
	l.seen[req.IdempotencyKey] = seenTransfer{req: req, t: result}
	return result, false, nil
}
