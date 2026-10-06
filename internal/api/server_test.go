package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/JohannsSatheesh/ledger/internal/ledger"
)

func do(t *testing.T, h http.Handler, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func setup(t *testing.T) http.Handler {
	t.Helper()
	h := NewServer(ledger.New())
	for _, body := range []string{
		`{"id":"alice","opening_balance":1000}`,
		`{"id":"bob","opening_balance":0}`,
	} {
		if rec := do(t, h, "POST", "/accounts", body, ""); rec.Code != http.StatusCreated {
			t.Fatalf("setup: POST /accounts = %d, body %s", rec.Code, rec.Body)
		}
	}
	return h
}

func balanceOf(t *testing.T, h http.Handler, id string) int64 {
	t.Helper()
	rec := do(t, h, "GET", "/accounts/"+id, "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /accounts/%s = %d", id, rec.Code)
	}
	var out struct {
		Balance int64 `json:"balance"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out.Balance
}

func TestCreateAccountDuplicate(t *testing.T) {
	h := setup(t)
	if rec := do(t, h, "POST", "/accounts", `{"id":"alice","opening_balance":5}`, ""); rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409", rec.Code)
	}
}

func TestCreateAccountBadJSON(t *testing.T) {
	h := NewServer(ledger.New())
	if rec := do(t, h, "POST", "/accounts", `{nope`, ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestGetAccountNotFound(t *testing.T) {
	h := NewServer(ledger.New())
	if rec := do(t, h, "GET", "/accounts/ghost", "", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestTransferFlow(t *testing.T) {
	h := setup(t)
	body := `{"from":"alice","to":"bob","amount":250}`

	rec := do(t, h, "POST", "/transfers", body, "k1")
	if rec.Code != http.StatusCreated {
		t.Fatalf("first transfer = %d, want 201 (body %s)", rec.Code, rec.Body)
	}
	first := rec.Body.String()

	rec = do(t, h, "POST", "/transfers", body, "k1")
	if rec.Code != http.StatusOK {
		t.Fatalf("replay = %d, want 200", rec.Code)
	}
	if rec.Body.String() != first {
		t.Fatalf("replay body differs:\n got  %s\n want %s", rec.Body, first)
	}

	if got := balanceOf(t, h, "alice"); got != 750 {
		t.Fatalf("alice = %d, want 750", got)
	}
	if got := balanceOf(t, h, "bob"); got != 250 {
		t.Fatalf("bob = %d, want 250", got)
	}
}

func TestTransferErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
		key  string
		want int
	}{
		{"missing key", `{"from":"alice","to":"bob","amount":5}`, "", http.StatusBadRequest},
		{"bad json", `{nope`, "k", http.StatusBadRequest},
		{"zero amount", `{"from":"alice","to":"bob","amount":0}`, "k", http.StatusBadRequest},
		{"unknown account", `{"from":"alice","to":"ghost","amount":5}`, "k", http.StatusNotFound},
		{"insufficient funds", `{"from":"alice","to":"bob","amount":999999}`, "k", http.StatusUnprocessableEntity},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := setup(t)
			if rec := do(t, h, "POST", "/transfers", tc.body, tc.key); rec.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", rec.Code, tc.want, rec.Body)
			}
		})
	}
}

func TestTransferKeyReuseConflict(t *testing.T) {
	h := setup(t)
	if rec := do(t, h, "POST", "/transfers", `{"from":"alice","to":"bob","amount":10}`, "k"); rec.Code != http.StatusCreated {
		t.Fatalf("first = %d", rec.Code)
	}
	if rec := do(t, h, "POST", "/transfers", `{"from":"alice","to":"bob","amount":20}`, "k"); rec.Code != http.StatusConflict {
		t.Fatalf("reuse = %d, want 409", rec.Code)
	}
}
