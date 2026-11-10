package platform

import (
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

func (p *Platform) adminOverview(w http.ResponseWriter, r *http.Request, viewer *studio) {
	if viewer == nil || viewer.role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	rows, e := p.db.Query("SELECT id,username,role,created,COALESCE(email,''),email_verified,disabled FROM accounts ORDER BY created DESC")
	if e != nil {
		fail(w, 500, "Unable to load accounts.")
		return
	}
	accounts := []map[string]any{}
	for rows.Next() {
		var id, name, role, created, email string
		var verified, disabled int
		if rows.Scan(&id, &name, &role, &created, &email, &verified, &disabled) != nil {
			rows.Close()
			fail(w, 500, "Unable to load accounts.")
			return
		}
		accounts = append(accounts, map[string]any{"id": id, "username": name, "role": role, "created": created, "email": email, "verified": verified == 1, "disabled": disabled == 1})
	}
	if rows.Err() != nil {
		rows.Close()
		fail(w, 500, "Unable to load accounts.")
		return
	}
	rows.Close()
	totals := map[string]int64{"galleries": 0, "photos": 0, "orders": 0, "paid": 0, "pending_orders": 0, "pending_withdrawals": 0}
	for _, a := range accounts {
		p.mu.RLock()
		s := p.studios[a["id"].(string)]
		p.mu.RUnlock()
		if s == nil {
			fail(w, 500, "A studio is unavailable.")
			return
		}
		var galleries, photos, orders, paid, pending, withdrawals int64
		e = s.db.QueryRow(`SELECT (SELECT count(*) FROM galleries WHERE archived=0),(SELECT count(*) FROM photos WHERE archived=0),(SELECT count(*) FROM orders),(SELECT COALESCE(sum(amount),0) FROM orders WHERE status='Paid'),(SELECT count(*) FROM orders WHERE status='Pending'),(SELECT count(*) FROM withdrawals WHERE status IN ('pending','otp'))`).Scan(&galleries, &photos, &orders, &paid, &pending, &withdrawals)
		if e != nil {
			fail(w, 500, "Unable to load platform totals.")
			return
		}
		a["galleries"], a["photos"], a["orders"], a["paid"], a["pending_orders"], a["pending_withdrawals"] = galleries, photos, orders, paid, pending, withdrawals
		totals["galleries"] += galleries
		totals["photos"] += photos
		totals["orders"] += orders
		totals["paid"] += paid
		totals["pending_orders"] += pending
		totals["pending_withdrawals"] += withdrawals
	}
	auditRows, e := p.db.Query("SELECT actor,action,target,created FROM admin_audit ORDER BY created DESC LIMIT 50")
	if e != nil {
		fail(w, 500, "Unable to load audit history.")
		return
	}
	audit := []map[string]string{}
	for auditRows.Next() {
		var actor, action, target, created string
		if auditRows.Scan(&actor, &action, &target, &created) != nil {
			auditRows.Close()
			fail(w, 500, "Unable to load audit history.")
			return
		}
		audit = append(audit, map[string]string{"actor": actor, "action": action, "target": target, "created": created})
	}
	auditRows.Close()
	reply(w, 200, map[string]any{"totals": totals, "accounts": accounts, "audit": audit})
}
func (p *Platform) adminStatus(w http.ResponseWriter, r *http.Request, viewer *studio) {
	if viewer == nil || viewer.role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var b struct {
		ID       string
		Disabled bool
	}
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b) != nil || b.ID == "" {
		fail(w, 400, "Invalid account.")
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	target := p.studios[b.ID]
	if target == nil || target.role != "photographer" {
		fail(w, 404, "Photographer account not found.")
		return
	}
	version := uuid.NewString()
	tx, e := p.db.Begin()
	if e != nil {
		fail(w, 500, "Could not update account.")
		return
	}
	defer tx.Rollback()
	disabled := 0
	action := "account enabled"
	if b.Disabled {
		disabled = 1
		action = "account suspended"
	}
	_, e = tx.Exec("UPDATE accounts SET disabled=?,session_version=? WHERE id=? AND role='photographer'", disabled, version, b.ID)
	if e == nil && b.Disabled {
		_, e = tx.Exec("DELETE FROM account_links WHERE account_id=?", b.ID)
	}
	if e == nil {
		_, e = tx.Exec("INSERT INTO admin_audit VALUES(?,?,?,?,?)", uuid.NewString(), viewer.username, action, target.username, time.Now().UTC().Format(time.RFC3339Nano))
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		fail(w, 500, "Could not update account.")
		return
	}
	target.version = version
	reply(w, 200, map[string]bool{"disabled": b.Disabled})
}
func (p *Platform) adminActivity(w http.ResponseWriter, r *http.Request, viewer *studio) {
	if viewer == nil || viewer.role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	id := r.URL.Query().Get("account")
	p.mu.RLock()
	target := p.studios[id]
	p.mu.RUnlock()
	if target == nil {
		fail(w, 404, "Account not found.")
		return
	}
	if _, e := p.db.Exec("INSERT INTO admin_audit VALUES(?,?,?,?,?)", uuid.NewString(), viewer.username, "viewed account activity", target.username, time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
		fail(w, 500, "Unable to record access.")
		return
	}
	orders := []map[string]any{}
	rows, e := target.db.Query("SELECT o.id,COALESCE(g.name,'Removed gallery'),o.email,o.amount,o.status,o.created FROM orders o LEFT JOIN galleries g ON g.id=o.gallery_id ORDER BY o.created DESC LIMIT 100")
	if e != nil {
		fail(w, 500, "Unable to load orders.")
		return
	}
	for rows.Next() {
		var id, name, email, status, created string
		var amount int64
		if rows.Scan(&id, &name, &email, &amount, &status, &created) != nil {
			rows.Close()
			fail(w, 500, "Unable to load orders.")
			return
		}
		orders = append(orders, map[string]any{"id": id, "gallery": name, "email": email, "amount": amount, "status": status, "created": created})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		fail(w, 500, "Unable to load orders.")
		return
	}
	withdrawals := []map[string]any{}
	rows, e = target.db.Query("SELECT reference,bank,account_last4,amount,status,created FROM withdrawals ORDER BY created DESC LIMIT 100")
	if e != nil {
		fail(w, 500, "Unable to load withdrawals.")
		return
	}
	for rows.Next() {
		var ref, bank, last, status, created string
		var amount int64
		if rows.Scan(&ref, &bank, &last, &amount, &status, &created) != nil {
			rows.Close()
			fail(w, 500, "Unable to load withdrawals.")
			return
		}
		withdrawals = append(withdrawals, map[string]any{"reference": ref, "bank": bank, "last4": last, "amount": amount, "status": status, "created": created})
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		fail(w, 500, "Unable to load withdrawals.")
		return
	}
	reply(w, 200, map[string]any{"username": target.username, "orders": orders, "withdrawals": withdrawals, "limit": 100})
}
func (p *Platform) adminSync(w http.ResponseWriter, r *http.Request, viewer *studio) {
	if viewer == nil || viewer.role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	var b struct{ AccountID, Reference, Kind string }
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b) != nil || !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(b.Reference) {
		fail(w, 400, "Invalid request.")
		return
	}
	p.mu.RLock()
	target := p.studios[b.AccountID]
	p.mu.RUnlock()
	if target == nil {
		fail(w, 404, "Account not found.")
		return
	}
	var query, path, action string
	switch b.Kind {
	case "order":
		query = "SELECT 1 FROM orders WHERE id=?"
		path = "/api/orders/" + b.Reference + "/sync"
		action = "refreshed order"
	case "withdrawal":
		query = "SELECT 1 FROM withdrawals WHERE reference=?"
		path = "/api/withdrawals/" + b.Reference + "/sync"
		action = "refreshed withdrawal"
	default:
		fail(w, 400, "Invalid request.")
		return
	}
	var n int
	if target.db.QueryRow(query, b.Reference).Scan(&n) != nil {
		fail(w, 404, "Record not found for this studio.")
		return
	}
	if _, e := p.db.Exec("INSERT INTO admin_audit VALUES(?,?,?,?,?)", uuid.NewString(), viewer.username, action, target.username+" · "+b.Reference, time.Now().UTC().Format(time.RFC3339Nano)); e != nil {
		fail(w, 500, "Unable to record action.")
		return
	}
	req := r.Clone(r.Context())
	u := *r.URL
	u.Path = path
	u.RawPath = ""
	u.RawQuery = ""
	req.URL = &u
	req.Body = io.NopCloser(strings.NewReader("{}"))
	p.forward(w, req, target, target)
}
