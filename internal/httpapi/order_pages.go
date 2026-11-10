package httpapi

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

// pagedOrders is opt-in so existing API clients keep their original response.
func (s *Server) pagedOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit, e := strconv.Atoi(q.Get("limit"))
	if e != nil || limit < 1 || limit > 100 {
		fail(w, 400, "Limit must be between 1 and 100.")
		return
	}
	offset := 0
	if q.Get("offset") != "" {
		offset, e = strconv.Atoi(q.Get("offset"))
		if e != nil || offset < 0 || offset > 1000000 {
			fail(w, 400, "Invalid offset.")
			return
		}
	}
	where := []string{"1=1"}
	args := []any{}
	status := q.Get("status")
	if status != "" {
		if status != "Paid" && status != "Pending" && status != "Failed" {
			fail(w, 400, "Invalid order status.")
			return
		}
		where = append(where, "o.status=?")
		args = append(args, status)
	}
	if gallery := q.Get("gallery"); gallery != "" {
		if len(gallery) > 100 {
			fail(w, 400, "Invalid gallery.")
			return
		}
		where = append(where, "o.gallery_id=?")
		args = append(args, gallery)
	}
	for _, field := range []struct{ name, op string }{{"from", ">="}, {"to", "<"}} {
		if v := q.Get(field.name); v != "" {
			t, err := time.Parse(time.RFC3339, v)
			if err != nil {
				fail(w, 400, "Invalid date boundary.")
				return
			}
			where = append(where, "o.created "+field.op+" ?")
			args = append(args, t.UTC().Format("2006-01-02T15:04:05"))
		}
	}
	clause := strings.Join(where, " AND ")
	base := " FROM orders o JOIN galleries g ON g.id=o.gallery_id WHERE " + clause
	var total, paid, pending, gross, fees int64
	e = s.DB.QueryRow("SELECT count(*),COALESCE(sum(CASE WHEN o.status='Paid' THEN 1 ELSE 0 END),0),COALESCE(sum(CASE WHEN o.status='Pending' THEN 1 ELSE 0 END),0),COALESCE(sum(CASE WHEN o.status='Paid' THEN o.amount ELSE 0 END),0),COALESCE(sum(CASE WHEN o.status='Paid' THEN o.platform_fee ELSE 0 END),0)"+base, args...).Scan(&total, &paid, &pending, &gross, &fees)
	if e != nil {
		fail(w, 500, "Unable to load order totals.")
		return
	}
	pageArgs := append(append([]any{}, args...), limit, offset)
	items, e := s.query("SELECT o.*,g.name gallery_name"+base+" ORDER BY o.created DESC,o.id DESC LIMIT ? OFFSET ?", pageArgs...)
	if e != nil {
		fail(w, 500, "Unable to load orders.")
		return
	}
	dayRows, e := s.query("SELECT substr(o.created,1,10) day,SUM(o.amount) amount"+base+" AND o.status='Paid' GROUP BY substr(o.created,1,10) ORDER BY day DESC LIMIT 12", args...)
	if e != nil {
		fail(w, 500, "Unable to load sales trends.")
		return
	}
	days := make([][]any, 0, len(dayRows))
	for i := len(dayRows) - 1; i >= 0; i-- {
		days = append(days, []any{dayRows[i]["day"], dayRows[i]["amount"]})
	}
	top, e := s.query("SELECT o.gallery_id id,g.name name,SUM(o.amount) total,COUNT(*) count"+base+" AND o.status='Paid' GROUP BY o.gallery_id,g.name ORDER BY total DESC,o.gallery_id LIMIT 5", args...)
	if e != nil {
		fail(w, 500, "Unable to load gallery insights.")
		return
	}
	choices, e := s.query("SELECT DISTINCT o.gallery_id id,g.name name FROM orders o JOIN galleries g ON g.id=o.gallery_id ORDER BY g.name,o.gallery_id")
	if e != nil {
		fail(w, 500, "Unable to load galleries.")
		return
	}
	average := int64(0)
	if paid > 0 {
		average = (gross + paid/2) / paid
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "offset": offset, "limit": limit, "has_more": int64(offset+len(items)) < total, "galleries": choices, "insights": map[string]any{"paid": paid, "pending": pending, "gross": gross, "net": gross - fees, "average": average, "days": days, "galleries": top}})
}
