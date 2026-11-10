package httpapi

import (
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) pagedGalleries(w http.ResponseWriter, r *http.Request) {
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
	where := []string{"g.archived=0"}
	args := []any{}
	if status := q.Get("status"); status != "" {
		if status != "Published" && status != "Draft" {
			fail(w, 400, "Invalid gallery status.")
			return
		}
		where = append(where, "g.status=?")
		args = append(args, status)
	}
	if term := strings.TrimSpace(q.Get("q")); term != "" {
		if len(term) > 200 {
			fail(w, 400, "Search is too long.")
			return
		}
		term = strings.ReplaceAll(term, "\\", "\\\\")
		term = strings.ReplaceAll(term, "%", "\\%")
		term = strings.ReplaceAll(term, "_", "\\_")
		where = append(where, `(lower(g.name) LIKE lower(?) ESCAPE '\' OR lower(COALESCE(g.client,'')) LIKE lower(?) ESCAPE '\' OR lower(COALESCE(g.location,'')) LIKE lower(?) ESCAPE '\')`)
		args = append(args, "%"+term+"%", "%"+term+"%", "%"+term+"%")
	}
	clause := strings.Join(where, " AND ")
	var total, photos int64
	e = s.DB.QueryRow("SELECT count(*),COALESCE(sum((SELECT count(*) FROM photos p WHERE p.gallery_id=g.id AND p.archived=0)),0) FROM galleries g WHERE "+clause, args...).Scan(&total, &photos)
	if e != nil {
		fail(w, 500, "Unable to load gallery totals.")
		return
	}
	sort := q.Get("sort")
	order := "g.created DESC,g.id DESC"
	switch sort {
	case "", "newest":
	case "oldest":
		order = "g.created ASC,g.id ASC"
	case "name":
		order = "g.name COLLATE NOCASE ASC,g.id ASC"
	case "photos":
		order = "photo_count DESC,g.created DESC,g.id DESC"
	default:
		fail(w, 400, "Invalid gallery sort.")
		return
	}
	pageArgs := append(append([]any{}, args...), limit, offset)
	query := `SELECT g.*, (SELECT count(*) FROM photos p WHERE p.gallery_id=g.id AND p.archived=0) photo_count, COALESCE(NULLIF(g.cover_photo,''),(SELECT p.id FROM photos p WHERE p.gallery_id=g.id AND p.archived=0 ORDER BY p.created LIMIT 1)) cover FROM galleries g WHERE ` + clause + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	items, e := s.query(query, pageArgs...)
	if e != nil {
		fail(w, 500, "Unable to load galleries.")
		return
	}
	respond(w, 200, map[string]any{"items": items, "total": total, "photo_total": photos, "offset": offset, "limit": limit, "has_more": int64(offset+len(items)) < total})
}
