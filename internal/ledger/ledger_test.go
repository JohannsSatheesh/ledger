package ledger

import (
	"errors"
	"fmt"
	"math/rand"
	"sync"
	"testing"
)

func newLedger(t *testing.T, balances map[AccountID]Money) *Ledger {
	t.Helper()
	l := New()
	for id, b := range balances {
		if err := l.CreateAccount(id, b); err != nil {
			t.Fatalf("CreateAccount(%q, %d): %v", id, b, err)
		}
	}
	return l
}

func mustBalance(t *testing.T, l *Ledger, id AccountID) Money {
	t.Helper()
	b, err := l.Balance(id)
	if err != nil {
		t.Fatalf("Balance(%q): %v", id, err)
	}
	return b
}

func TestCreateAccount(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(l *Ledger)
		id      AccountID
		opening Money
		wantErr error
	}{
		{name: "ok", id: "alice", opening: 1000},
		{name: "zero opening is fine", id: "alice", opening: 0},
		{name: "negative opening", id: "alice", opening: -1, wantErr: ErrInvalidOpening},
		{
			name:    "duplicate",
			setup:   func(l *Ledger) { _ = l.CreateAccount("alice", 5) },
			id:      "alice",
			opening: 10,
			wantErr: ErrAccountExists,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := New()
			if tc.setup != nil {
				tc.setup(l)
			}
			err := l.CreateAccount(tc.id, tc.opening)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr == nil {
				if got := mustBalance(t, l, tc.id); got != tc.opening {
					t.Fatalf("balance = %d, want %d", got, tc.opening)
				}
			}
		})
	}
}

func TestBalanceUnknownAccount(t *testing.T) {
	if _, err := New().Balance("nobody"); !errors.Is(err, ErrAccountNotFound) {
		t.Fatalf("err = %v, want ErrAccountNotFound", err)
	}
}

func TestTransfer(t *testing.T) {
	tests := []struct {
		name      string
		req       TransferRequest
		wantErr   error
		wantAlice Money
		wantBob   Money
	}{
		{
			name:      "ok",
			req:       TransferRequest{From: "alice", To: "bob", Amount: 250, IdempotencyKey: "k1"},
			wantAlice: 750, wantBob: 250,
		},
		{
			name:      "whole balance",
			req:       TransferRequest{From: "alice", To: "bob", Amount: 1000, IdempotencyKey: "k1"},
			wantAlice: 0, wantBob: 1000,
		},
		{
			name:      "insufficient funds",
			req:       TransferRequest{From: "alice", To: "bob", Amount: 1001, IdempotencyKey: "k1"},
			wantErr:   ErrInsufficientFunds,
			wantAlice: 1000, wantBob: 0,
		},
		{
			name:      "zero amount",
			req:       TransferRequest{From: "alice", To: "bob", Amount: 0, IdempotencyKey: "k1"},
			wantErr:   ErrInvalidAmount,
			wantAlice: 1000, wantBob: 0,
		},
		{
			name:      "negative amount",
			req:       TransferRequest{From: "alice", To: "bob", Amount: -5, IdempotencyKey: "k1"},
			wantErr:   ErrInvalidAmount,
			wantAlice: 1000, wantBob: 0,
		},
		{
			name:      "same account",
			req:       TransferRequest{From: "alice", To: "alice", Amount: 5, IdempotencyKey: "k1"},
			wantErr:   ErrSameAccount,
			wantAlice: 1000, wantBob: 0,
		},
		{
			name:      "unknown sender",
			req:       TransferRequest{From: "ghost", To: "bob", Amount: 5, IdempotencyKey: "k1"},
			wantErr:   ErrAccountNotFound,
			wantAlice: 1000, wantBob: 0,
		},
		{
			name:      "unknown recipient",
			req:       TransferRequest{From: "alice", To: "ghost", Amount: 5, IdempotencyKey: "k1"},
			wantErr:   ErrAccountNotFound,
			wantAlice: 1000, wantBob: 0,
		},
		{
			name:      "missing idempotency key",
			req:       TransferRequest{From: "alice", To: "bob", Amount: 5},
			wantErr:   ErrMissingKey,
			wantAlice: 1000, wantBob: 0,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			l := newLedger(t, map[AccountID]Money{"alice": 1000, "bob": 0})
			tr, replayed, err := l.Transfer(tc.req)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if replayed {
				t.Fatal("first use of a key must not be reported as a replay")
			}
			if tc.wantErr == nil && (tr.ID == "" || tr.Amount != tc.req.Amount) {
				t.Fatalf("unexpected transfer result: %+v", tr)
			}
			if got := mustBalance(t, l, "alice"); got != tc.wantAlice {
				t.Errorf("alice = %d, want %d", got, tc.wantAlice)
			}
			if got := mustBalance(t, l, "bob"); got != tc.wantBob {
				t.Errorf("bob = %d, want %d", got, tc.wantBob)
			}
			if got := l.Total(); got != 1000 {
				t.Errorf("total = %d, want 1000 (money was created or destroyed)", got)
			}
		})
	}
}

func TestTransferIdempotentReplay(t *testing.T) {
	l := newLedger(t, map[AccountID]Money{"alice": 1000, "bob": 0})
	req := TransferRequest{From: "alice", To: "bob", Amount: 300, IdempotencyKey: "pay-1"}

	first, replayed, err := l.Transfer(req)
	if err != nil || replayed {
		t.Fatalf("first call: replayed=%v err=%v", replayed, err)
	}
	second, replayed, err := l.Transfer(req)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if !replayed {
		t.Fatal("second call with the same key should be reported as a replay")
	}
	if first != second {
		t.Fatalf("replay returned %+v, want original %+v", second, first)
	}
	if got := mustBalance(t, l, "alice"); got != 700 {
		t.Fatalf("alice = %d, want 700 (money moved twice?)", got)
	}
}

func TestTransferIdempotencyConflict(t *testing.T) {
	l := newLedger(t, map[AccountID]Money{"alice": 1000, "bob": 0})
	_, _, err := l.Transfer(TransferRequest{From: "alice", To: "bob", Amount: 300, IdempotencyKey: "pay-1"})
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = l.Transfer(TransferRequest{From: "alice", To: "bob", Amount: 400, IdempotencyKey: "pay-1"})
	if !errors.Is(err, ErrIdempotencyConflict) {
		t.Fatalf("err = %v, want ErrIdempotencyConflict", err)
	}
	if got := mustBalance(t, l, "alice"); got != 700 {
		t.Fatalf("alice = %d, want 700", got)
	}
}

// A replay of a payment that succeeded must still succeed even if the sender
// can no longer afford it today.
func TestReplayWinsOverInsufficientFunds(t *testing.T) {
	l := newLedger(t, map[AccountID]Money{"alice": 100, "bob": 0, "carol": 0})
	req := TransferRequest{From: "alice", To: "bob", Amount: 100, IdempotencyKey: "pay-1"}
	if _, _, err := l.Transfer(req); err != nil {
		t.Fatal(err)
	}
	_, replayed, err := l.Transfer(req)
	if err != nil || !replayed {
		t.Fatalf("replay: replayed=%v err=%v, want a clean replay", replayed, err)
	}
}

// Many goroutines hammer the ledger with random transfers. Whatever happens,
// money must be conserved and no balance may go negative.
// Run with: go test -race ./...
func TestConcurrentTransfersConserveMoney(t *testing.T) {
	const (
		accounts   = 8
		opening    = Money(1000)
		goroutines = 100
		perRoutine = 200
	)
	l := New()
	ids := make([]AccountID, accounts)
	for i := range ids {
		ids[i] = AccountID(fmt.Sprintf("acc-%d", i))
		if err := l.CreateAccount(ids[i], opening); err != nil {
			t.Fatal(err)
		}
	}

	var wg sync.WaitGroup
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			rng := rand.New(rand.NewSource(int64(g)))
			for i := 0; i < perRoutine; i++ {
				from := ids[rng.Intn(accounts)]
				to := ids[rng.Intn(accounts)]
				_, _, err := l.Transfer(TransferRequest{
					From: from, To: to,
					Amount:         Money(1 + rng.Intn(500)),
					IdempotencyKey: fmt.Sprintf("g%d-i%d", g, i),
				})
				// These are expected under random load; anything else is a bug.
				if err != nil && !errors.Is(err, ErrInsufficientFunds) && !errors.Is(err, ErrSameAccount) {
					t.Errorf("unexpected error: %v", err)
					return
				}
			}
		}(g)
	}
	wg.Wait()

	if got, want := l.Total(), opening*accounts; got != want {
		t.Fatalf("total = %d, want %d: money was created or destroyed", got, want)
	}
	for _, id := range ids {
		if b := mustBalance(t, l, id); b < 0 {
			t.Fatalf("%s has negative balance %d", id, b)
		}
	}
}

// 50 identical requests at once: exactly one transfer may happen.
func TestConcurrentSameKeyMovesMoneyOnce(t *testing.T) {
	const n = 50
	l := newLedger(t, map[AccountID]Money{"alice": 1000, "bob": 0})
	req := TransferRequest{From: "alice", To: "bob", Amount: 100, IdempotencyKey: "same-key"}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		ids     = map[string]int{}
		freshes int
	)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tr, replayed, err := l.Transfer(req)
			if err != nil {
				t.Errorf("Transfer: %v", err)
				return
			}
			mu.Lock()
			ids[tr.ID]++
			if !replayed {
				freshes++
			}
			mu.Unlock()
		}()
	}
	wg.Wait()

	if len(ids) != 1 {
		t.Fatalf("got %d distinct transfer IDs, want 1: %v", len(ids), ids)
	}
	if freshes != 1 {
		t.Fatalf("%d calls were not replays, want exactly 1", freshes)
	}
	if got := mustBalance(t, l, "alice"); got != 900 {
		t.Fatalf("alice = %d, want 900", got)
	}
}
