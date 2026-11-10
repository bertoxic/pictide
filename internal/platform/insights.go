package platform

import (
	"net/http"
	"sort"
	"time"
)

// adminInsights summarizes the application's own records, not Paystack settlement.
func (p *Platform) adminInsights(w http.ResponseWriter, r *http.Request, viewer *studio) {
	if viewer == nil || viewer.role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	if r.Method != "GET" {
		w.WriteHeader(405)
		return
	}
	p.mu.RLock()
	studios := make([]*studio, 0, len(p.studios))
	for _, s := range p.studios {
		studios = append(studios, s)
	}
	p.mu.RUnlock()
	totals := map[string]int64{"paid_orders": 0, "gross": 0, "platform_fees": 0, "photographer_earnings": 0, "held_withdrawals": 0, "completed_withdrawals": 0, "pending_orders": 0, "overdue_orders": 0, "pending_contributions": 0, "published_galleries": 0}
	days := map[string]map[string]int64{}
	now := time.Now().UTC()
	start := now.AddDate(0, 0, -89).Format("2006-01-02")
	old := now.Add(-24 * time.Hour).Format(time.RFC3339Nano)
	orders := []map[string]any{}
	withdrawals := []map[string]any{}
	for _, s := range studios {
		var paid, gross, fees, held, done, pending, overdue, uploads, published int64
		e := s.db.QueryRow(`SELECT (SELECT count(*) FROM orders o JOIN payment_events pe ON pe.reference=o.id WHERE o.status='Paid' AND pe.provider='paystack'),(SELECT COALESCE(sum(o.amount),0) FROM orders o JOIN payment_events pe ON pe.reference=o.id WHERE o.status='Paid' AND pe.provider='paystack'),(SELECT COALESCE(sum(o.platform_fee),0) FROM orders o JOIN payment_events pe ON pe.reference=o.id WHERE o.status='Paid' AND pe.provider='paystack'),(SELECT COALESCE(sum(amount),0) FROM withdrawals WHERE status IN ('pending','otp')),(SELECT COALESCE(sum(amount),0) FROM withdrawals WHERE status='success'),(SELECT count(*) FROM orders WHERE status='Pending'),(SELECT count(*) FROM orders WHERE status='Pending' AND created < ?),(SELECT count(*) FROM guest_uploads WHERE status='pending'),(SELECT count(*) FROM galleries WHERE status='Published' AND archived=0)`, old).Scan(&paid, &gross, &fees, &held, &done, &pending, &overdue, &uploads, &published)
		if e != nil {
			fail(w, 500, "Unable to load platform analytics.")
			return
		}
		totals["paid_orders"] += paid
		totals["gross"] += gross
		totals["platform_fees"] += fees
		totals["photographer_earnings"] += gross - fees
		totals["held_withdrawals"] += held
		totals["completed_withdrawals"] += done
		totals["pending_orders"] += pending
		totals["overdue_orders"] += overdue
		totals["pending_contributions"] += uploads
		totals["published_galleries"] += published
		rows, e := s.db.Query(`SELECT substr(o.created,1,10),count(*),COALESCE(sum(o.amount),0) FROM orders o JOIN payment_events pe ON pe.reference=o.id WHERE o.status='Paid' AND pe.provider='paystack' AND substr(o.created,1,10)>=? GROUP BY substr(o.created,1,10)`, start)
		if e != nil {
			fail(w, 500, "Unable to load sales trends.")
			return
		}
		for rows.Next() {
			var day string
			var count, amount int64
			if rows.Scan(&day, &count, &amount) != nil {
				rows.Close()
				fail(w, 500, "Unable to load sales trends.")
				return
			}
			if days[day] == nil {
				days[day] = map[string]int64{"count": 0, "amount": 0}
			}
			days[day]["count"] += count
			days[day]["amount"] += amount
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			fail(w, 500, "Unable to load sales trends.")
			return
		}
		rows, e = s.db.Query(`SELECT o.id,COALESCE(g.name,'Removed gallery'),o.amount,o.status,o.created FROM orders o LEFT JOIN galleries g ON g.id=o.gallery_id ORDER BY o.created DESC LIMIT 200`)
		if e != nil {
			fail(w, 500, "Unable to load recent orders.")
			return
		}
		for rows.Next() {
			var id, gallery, status, created string
			var amount int64
			if rows.Scan(&id, &gallery, &amount, &status, &created) != nil {
				rows.Close()
				fail(w, 500, "Unable to load recent orders.")
				return
			}
			orders = append(orders, map[string]any{"id": id, "account_id": s.id, "account": s.username, "gallery": gallery, "amount": amount, "status": status, "created": created})
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			fail(w, 500, "Unable to load recent orders.")
			return
		}
		rows, e = s.db.Query(`SELECT reference,amount,status,created,account_last4 FROM withdrawals ORDER BY created DESC LIMIT 200`)
		if e != nil {
			fail(w, 500, "Unable to load recent withdrawals.")
			return
		}
		for rows.Next() {
			var ref, status, created, last4 string
			var amount int64
			if rows.Scan(&ref, &amount, &status, &created, &last4) != nil {
				rows.Close()
				fail(w, 500, "Unable to load recent withdrawals.")
				return
			}
			withdrawals = append(withdrawals, map[string]any{"reference": ref, "account_id": s.id, "account": s.username, "amount": amount, "status": status, "created": created, "last4": last4})
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			fail(w, 500, "Unable to load recent withdrawals.")
			return
		}
	}
	sort.Slice(orders, func(i, j int) bool { return orders[i]["created"].(string) > orders[j]["created"].(string) })
	if len(orders) > 200 {
		orders = orders[:200]
	}
	sort.Slice(withdrawals, func(i, j int) bool { return withdrawals[i]["created"].(string) > withdrawals[j]["created"].(string) })
	if len(withdrawals) > 200 {
		withdrawals = withdrawals[:200]
	}
	timeline := []map[string]any{}
	for i := 89; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		v := days[day]
		var count, amount int64
		if v != nil {
			count = v["count"]
			amount = v["amount"]
		}
		timeline = append(timeline, map[string]any{"date": day, "count": count, "amount": amount})
	}
	reply(w, 200, map[string]any{"totals": totals, "timeline": timeline, "orders": orders, "withdrawals": withdrawals, "limit": 200})
}
