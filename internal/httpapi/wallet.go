package httpapi

import (
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

func (s *Server) wallet(w http.ResponseWriter, r *http.Request) {
	var earned, held int64
	if s.DB.QueryRow("SELECT COALESCE(SUM(o.amount-o.platform_fee),0) FROM orders o JOIN payment_events pe ON pe.reference=o.id WHERE o.status='Paid' AND pe.provider='paystack'").Scan(&earned) != nil || s.DB.QueryRow("SELECT COALESCE(SUM(amount),0) FROM withdrawals WHERE status NOT IN ('failed','reversed')").Scan(&held) != nil {
		fail(w, 500, "Could not load wallet.")
		return
	}
	list, err := s.query("SELECT * FROM withdrawals ORDER BY created DESC")
	if err != nil {
		fail(w, 500, "Could not load wallet.")
		return
	}
	available := earned - held
	if available < 0 {
		available = 0
	}
	respond(w, 200, map[string]any{"earned": earned, "available": available, "feeBps": s.Config.FeeBPS, "withdrawals": list, "configured": s.Config.PaystackSecret != ""})
}
func (s *Server) banks(w http.ResponseWriter, r *http.Request) {
	var data []map[string]any
	if err := s.Paystack.Call(r.Context(), "GET", "/bank?country=nigeria&perPage=100", nil, &data); err != nil {
		fail(w, 502, "Unable to load banks from Paystack.")
		return
	}
	respond(w, 200, data)
}

var accountPattern = regexp.MustCompile(`^\d{10}$`)
var bankPattern = regexp.MustCompile(`^\d{2,6}$`)

func (s *Server) resolveAccount(r *http.Request, account, bank string) (map[string]any, error) {
	if !accountPattern.MatchString(account) || !bankPattern.MatchString(bank) {
		return nil, fmt.Errorf("invalid account")
	}
	var result map[string]any
	err := s.Paystack.Call(r.Context(), "GET", "/bank/resolve?account_number="+url.QueryEscape(account)+"&bank_code="+url.QueryEscape(bank), nil, &result)
	return result, err
}
func (s *Server) resolve(w http.ResponseWriter, r *http.Request) {
	data, err := s.resolveAccount(r, r.URL.Query().Get("account"), r.URL.Query().Get("bank"))
	if err != nil {
		fail(w, 400, "Could not resolve the bank account.")
		return
	}
	respond(w, 200, data)
}
func (s *Server) syncWithdrawal(w http.ResponseWriter, r *http.Request) {
	ref := chi.URLParam(r, "reference")
	var amount int64
	if s.DB.QueryRow("SELECT amount FROM withdrawals WHERE reference=?", ref).Scan(&amount) != nil {
		http.NotFound(w, r)
		return
	}
	var transfer struct {
		Reference, Status, Currency string
		Amount                      int64
	}
	if err := s.Paystack.Call(r.Context(), "GET", "/transfer/verify/"+url.PathEscape(ref), nil, &transfer); err != nil || transfer.Reference != ref || transfer.Amount != amount || transfer.Currency != "NGN" {
		fail(w, 502, "Could not verify transfer.")
		return
	}
	allowed := map[string]bool{"pending": true, "success": true, "failed": true, "reversed": true, "otp": true, "abandoned": true}
	if !allowed[transfer.Status] {
		fail(w, 502, "Transfer status needs manual reconciliation.")
		return
	}
	_, err := s.DB.Exec("UPDATE withdrawals SET status=? WHERE reference=? AND (status NOT IN ('success','reversed') OR ? IN ('success','reversed'))", transfer.Status, ref, transfer.Status)
	if err != nil {
		fail(w, 500, "Could not update transfer.")
		return
	}
	respond(w, 200, map[string]string{"status": transfer.Status})
}
func (s *Server) withdraw(w http.ResponseWriter, r *http.Request) {
	var b struct {
		Amount              int64
		Account, Bank, Name string
	}
	if decode(r, &b) != nil || b.Amount <= 0 || !accountPattern.MatchString(b.Account) || !bankPattern.MatchString(b.Bank) {
		fail(w, 400, "Invalid withdrawal details.")
		return
	}
	resolved, e := s.resolveAccount(r, b.Account, b.Bank)
	if e != nil || str(resolved["account_name"]) == "" || str(resolved["account_name"]) != b.Name {
		fail(w, 400, "Resolve and confirm the account name first.")
		return
	}
	var balance []struct {
		Currency string
		Balance  int64
	}
	if e = s.Paystack.Call(r.Context(), "GET", "/balance", nil, &balance); e != nil {
		fail(w, 502, "Could not confirm the Paystack balance.")
		return
	}
	sufficient := false
	for _, v := range balance {
		if v.Currency == "NGN" && v.Balance >= b.Amount {
			sufficient = true
		}
	}
	if !sufficient {
		fail(w, 400, "The settled Paystack balance is insufficient.")
		return
	}
	var recipient struct {
		Code string `json:"recipient_code"`
	}
	e = s.Paystack.Call(r.Context(), "POST", "/transferrecipient", map[string]any{"type": "nuban", "name": b.Name, "account_number": b.Account, "bank_code": b.Bank, "currency": "NGN"}, &recipient)
	if e != nil || recipient.Code == "" {
		fail(w, 502, "Could not create transfer recipient.")
		return
	}
	ref := "lumen-" + strings.ReplaceAll(uuid.NewString(), "-", "")
	tx, e := s.DB.BeginTx(r.Context(), nil)
	if e != nil {
		fail(w, 500, "Could not reserve funds.")
		return
	}
	defer tx.Rollback()
	var earned, held int64
	e = tx.QueryRow("SELECT COALESCE(SUM(o.amount-o.platform_fee),0) FROM orders o JOIN payment_events pe ON pe.reference=o.id WHERE o.status='Paid' AND pe.provider='paystack'").Scan(&earned)
	if e == nil {
		e = tx.QueryRow("SELECT COALESCE(SUM(amount),0) FROM withdrawals WHERE status NOT IN ('failed','reversed')").Scan(&held)
	}
	if e != nil {
		fail(w, 500, "Could not reserve funds.")
		return
	}
	if b.Amount > earned-held {
		fail(w, 400, "Insufficient available balance.")
		return
	}
	_, e = tx.Exec("INSERT INTO withdrawals VALUES(?,?,?,?,?,?,?)", uuid.NewString(), ref, b.Amount, b.Bank, b.Account[6:], "pending", now())
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		fail(w, 500, "Could not reserve funds.")
		return
	}
	var transfer struct{ Reference, Status string }
	e = s.Paystack.Call(r.Context(), "POST", "/transfer", map[string]any{"source": "balance", "amount": b.Amount, "recipient": recipient.Code, "reference": ref, "reason": "Pictide studio payout"}, &transfer)
	if e != nil || transfer.Reference != ref || !map[string]bool{"pending": true, "success": true, "otp": true}[transfer.Status] {
		respond(w, 502, map[string]string{"error": "Transfer outcome is unknown. Check Paystack before retrying.", "reference": ref})
		return
	}
	_, e = s.DB.Exec("UPDATE withdrawals SET status=? WHERE reference=?", transfer.Status, ref)
	if e != nil {
		fail(w, 500, "Transfer started; refresh status before retrying.")
		return
	}
	respond(w, 200, map[string]string{"reference": ref, "status": transfer.Status})
}
