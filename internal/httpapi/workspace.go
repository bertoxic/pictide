package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"strings"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"pictide.local/studio/internal/mongostore"
	"pictide.local/studio/internal/security"
)

func (s *Server) api(w http.ResponseWriter, r *http.Request, user *account) bool {
	switch {
	case r.URL.Path == "/api/admin/overview" && r.Method == "GET":
		s.adminOverview(w, r, user)
		return true
	case r.URL.Path == "/api/admin/accounts" && r.Method == "POST":
		s.createAccount(w, r, user)
		return true
	case r.URL.Path == "/api/admin/account-status" && r.Method == "POST":
		s.accountStatus(w, r, user)
		return true
	case r.URL.Path == "/api/security/password" && r.Method == "POST":
		s.changePassword(w, r, user)
		return true
	case r.URL.Path == "/api/settings" && r.Method == "GET":
		s.settings(w, r, user)
		return true
	case r.URL.Path == "/api/settings" && r.Method == "PUT":
		s.saveSettings(w, r, user)
		return true
	case r.URL.Path == "/api/galleries" && r.Method == "GET":
		s.listGalleries(w, r, user)
		return true
	case r.URL.Path == "/api/galleries" && r.Method == "POST":
		s.createGallery(w, r, user)
		return true
	case strings.HasPrefix(r.URL.Path, "/api/galleries/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/galleries/")
		if parts := strings.Split(id, "/"); len(parts) == 2 {
			switch {
			case parts[1] == "direct-upload" && r.Method == "POST":
				s.startDirectUpload(w, r, user, parts[0])
				return true
			case parts[1] == "cover" && r.Method == "PUT":
				s.cover(w, r, user, parts[0])
				return true
			case parts[1] == "customization" && r.Method == "PATCH":
				s.customize(w, r, user, parts[0])
				return true
			case parts[1] == "card-design" && r.Method == "PUT":
				s.cardDesign(w, r, user, parts[0])
				return true
			case parts[1] == "qr" && r.Method == "GET":
				s.galleryQR(w, r, user, parts[0])
				return true
			}
		}
		if strings.HasSuffix(id, "/photos") && r.Method == "POST" {
			galleryID := strings.TrimSuffix(id, "/photos")
			if galleryID != "" && !strings.Contains(galleryID, "/") {
				s.uploadPhotos(w, r, user, galleryID)
				return true
			}
		}
		if id == "" || strings.Contains(id, "/") {
			return false
		}
		switch r.Method {
		case "GET":
			s.getGallery(w, r, user, id)
		case "PATCH":
			s.updateGallery(w, r, user, id)
		case "DELETE":
			s.archiveGallery(w, r, user, id)
		default:
			return false
		}
		return true
	case r.URL.Path == "/api/orders" && r.Method == "GET":
		s.listOrders(w, r, user)
		return true
	case strings.HasPrefix(r.URL.Path, "/api/direct-uploads/") && strings.HasSuffix(r.URL.Path, "/complete") && r.Method == "POST":
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/direct-uploads/"), "/complete")
		if id == "" || strings.Contains(id, "/") {
			return false
		}
		s.completeDirectUpload(w, r, user, id)
		return true
	case strings.HasPrefix(r.URL.Path, "/api/orders/") && strings.HasSuffix(r.URL.Path, "/sync") && r.Method == "POST":
		id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/orders/"), "/sync")
		if id == "" || strings.Contains(id, "/") {
			return false
		}
		s.syncOrder(w, r, user, id)
		return true
	case strings.HasPrefix(r.URL.Path, "/api/photos/"):
		id := strings.TrimPrefix(r.URL.Path, "/api/photos/")
		if id == "" || strings.Contains(id, "/") {
			return false
		}
		if r.Method == "PATCH" {
			s.photoPrice(w, r, user, id)
			return true
		}
		if r.Method == "DELETE" {
			s.archivePhoto(w, r, user, id)
			return true
		}
		return false
	}
	return false
}

func (s *Server) adminOverview(w http.ResponseWriter, r *http.Request, user *account) {
	if user.Role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	ctx := r.Context()
	cur, err := s.db.Collection("accounts").Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "created", Value: -1}}))
	if err != nil {
		fail(w, 500, "Unable to load accounts.")
		return
	}
	defer cur.Close(ctx)
	var users []account
	if cur.All(ctx, &users) != nil {
		fail(w, 500, "Unable to load accounts.")
		return
	}
	accounts := []bson.M{}
	totals := bson.M{"galleries": int64(0), "photos": int64(0), "orders": int64(0), "paid": int64(0), "pending_orders": int64(0), "pending_withdrawals": int64(0)}
	for _, a := range users {
		g, e := s.db.Collection("galleries").CountDocuments(ctx, bson.M{"studio_id": a.ID, "archived": bson.M{"$ne": 1}})
		if e != nil {
			fail(w, 500, "Unable to load overview.")
			return
		}
		p, e := s.db.Collection("photos").CountDocuments(ctx, bson.M{"studio_id": a.ID, "archived": bson.M{"$ne": 1}, "ready": true})
		if e != nil {
			fail(w, 500, "Unable to load overview.")
			return
		}
		orders, e := s.readOrders(r, a.ID)
		if e != nil {
			fail(w, 500, "Unable to load overview.")
			return
		}
		var paid, pending int64
		for _, o := range orders {
			if o.Status == "Paid" {
				paid += o.Amount
			}
			if o.Status == "Pending" {
				pending++
			}
		}
		withdrawals, e := s.db.Collection("withdrawals").CountDocuments(ctx, bson.M{"studio_id": a.ID, "status": bson.M{"$in": bson.A{"pending", "otp"}}})
		if e != nil {
			fail(w, 500, "Unable to load overview.")
			return
		}
		accounts = append(accounts, bson.M{"id": a.ID, "username": a.Username, "role": a.Role, "disabled": a.Disabled, "created": a.Created, "galleries": g, "photos": p, "orders": len(orders), "pending_orders": pending, "paid": paid})
		totals["galleries"] = totals["galleries"].(int64) + g
		totals["photos"] = totals["photos"].(int64) + p
		totals["orders"] = totals["orders"].(int64) + int64(len(orders))
		totals["paid"] = totals["paid"].(int64) + paid
		totals["pending_orders"] = totals["pending_orders"].(int64) + pending
		totals["pending_withdrawals"] = totals["pending_withdrawals"].(int64) + withdrawals
	}
	reply(w, 200, bson.M{"accounts": accounts, "totals": totals, "audit": []any{}})
}

func (s *Server) createAccount(w http.ResponseWriter, r *http.Request, user *account) {
	if user.Role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	var b struct{ Username, Password string }
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid account.")
		return
	}
	b.Username = strings.ToLower(strings.TrimSpace(b.Username))
	if len(b.Username) < 3 || len(b.Username) > 64 || len(b.Password) < 12 || len(b.Password) > 200 {
		fail(w, 400, "Use a username of 3–64 characters and a password of at least 12 characters.")
		return
	}
	for i, c := range b.Username {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || i > 0 && (c == '.' || c == '_' || c == '-')) {
			fail(w, 400, "Invalid username.")
			return
		}
	}
	hash, err := security.PasswordHash(b.Password)
	if err != nil {
		fail(w, 500, "Unable to create account.")
		return
	}
	id, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to create account.")
		return
	}
	version, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to create account.")
		return
	}
	a := account{ID: id, Username: b.Username, PasswordHash: hash, Role: "photographer", Version: version, Created: timestamp()}
	if _, err = s.db.Collection("accounts").InsertOne(r.Context(), a); err != nil {
		if mongo.IsDuplicateKeyError(err) {
			fail(w, 409, "Username is already in use.")
		} else {
			fail(w, 500, "Unable to create account.")
		}
		return
	}
	reply(w, 201, bson.M{"id": id, "username": a.Username, "role": a.Role})
}

func (s *Server) settings(w http.ResponseWriter, r *http.Request, user *account) {
	var doc struct {
		StudioName   string  `bson:"studio_name"`
		ContactEmail string  `bson:"contact_email"`
		DefaultPrice float64 `bson:"default_price"`
	}
	err := s.db.Collection("settings").FindOne(r.Context(), bson.M{"_id": user.ID}).Decode(&doc)
	if errors.Is(err, mongo.ErrNoDocuments) {
		doc.StudioName = "Pictide Studio"
		doc.DefaultPrice = 5000
	} else if err != nil {
		fail(w, 500, "Unable to load settings.")
		return
	}
	reply(w, 200, bson.M{"studio_name": doc.StudioName, "contact_email": doc.ContactEmail, "default_price": doc.DefaultPrice, "studio_logo": false})
}

func (s *Server) saveSettings(w http.ResponseWriter, r *http.Request, user *account) {
	var b struct {
		StudioName   string `json:"studio_name"`
		ContactEmail string `json:"contact_email"`
		DefaultPrice any    `json:"default_price"`
	}
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid settings.")
		return
	}
	b.StudioName = strings.TrimSpace(b.StudioName)
	b.ContactEmail = strings.TrimSpace(b.ContactEmail)
	price, valid := number(b.DefaultPrice)
	if b.StudioName == "" || len(b.StudioName) > 80 || len(b.ContactEmail) > 254 || (b.ContactEmail != "" && (!strings.Contains(b.ContactEmail, "@") || strings.ContainsAny(b.ContactEmail, " \r\n"))) || !valid || price < 0 || price > 10000000 {
		fail(w, 400, "Enter a studio name and valid defaults.")
		return
	}
	_, err := s.db.Collection("settings").UpdateOne(r.Context(), bson.M{"_id": user.ID}, bson.M{"$set": bson.M{"studio_name": b.StudioName, "contact_email": b.ContactEmail, "default_price": price}}, options.UpdateOne().SetUpsert(true))
	if err != nil {
		fail(w, 500, "Unable to save settings.")
		return
	}
	reply(w, 200, bson.M{"ok": true})
}

func pagination(r *http.Request) (int64, int64, error) {
	limit := int64(30)
	offset := int64(0)
	var err error
	if v := r.URL.Query().Get("limit"); v != "" {
		limit, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, 0, err
		}
	}
	if v := r.URL.Query().Get("offset"); v != "" {
		offset, err = strconv.ParseInt(v, 10, 64)
		if err != nil {
			return 0, 0, err
		}
	}
	if limit < 1 || limit > 100 || offset < 0 || offset > 1000000 {
		return 0, 0, errors.New("invalid pagination")
	}
	return limit, offset, nil
}

func (s *Server) listGalleries(w http.ResponseWriter, r *http.Request, user *account) {
	limit, offset, err := pagination(r)
	if err != nil {
		fail(w, 400, "Invalid pagination.")
		return
	}
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	page, err := store.List(r.Context(), r.URL.Query().Get("q"), r.URL.Query().Get("status"), limit, offset)
	if err != nil {
		fail(w, 500, "Unable to load galleries.")
		return
	}
	var photoTotal int64
	for _, g := range page.Items {
		count, e := s.db.Collection("photos").CountDocuments(r.Context(), bson.M{"studio_id": user.ID, "gallery_id": g["id"], "archived": bson.M{"$ne": 1}, "ready": true})
		if e != nil {
			fail(w, 500, "Unable to load galleries.")
			return
		}
		g["photo_count"] = count
		photoTotal += count
		g["cover"] = g["cover_photo"]
	}
	reply(w, 200, bson.M{"items": page.Items, "total": page.Total, "photo_total": photoTotal, "offset": offset, "limit": limit, "has_more": offset+int64(len(page.Items)) < page.Total})
}

func number(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, !math.IsNaN(n) && !math.IsInf(n, 0)
	case string:
		f, e := strconv.ParseFloat(n, 64)
		return f, e == nil && !math.IsNaN(f) && !math.IsInf(f, 0)
	case int64:
		return float64(n), true
	case int32:
		return float64(n), true
	}
	return 0, false
}
func stringValue(v any) string { x, _ := v.(string); return x }

func (s *Server) createGallery(w http.ResponseWriter, r *http.Request, user *account) {
	var b map[string]any
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	name := strings.TrimSpace(stringValue(b["name"]))
	price, ok := number(b["price"])
	if _, exists := b["price"]; !exists {
		var setting struct {
			DefaultPrice float64 `bson:"default_price"`
		}
		e := s.db.Collection("settings").FindOne(r.Context(), bson.M{"_id": user.ID}).Decode(&setting)
		if errors.Is(e, mongo.ErrNoDocuments) {
			price = 5000
			ok = true
		} else if e == nil {
			price = setting.DefaultPrice
			ok = true
		}
	}
	if name == "" || len(name) > 200 || !ok || price < 0 || price > 10000000 {
		fail(w, 400, "A gallery name and valid price are required.")
		return
	}
	id, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to create gallery.")
		return
	}
	fields := bson.M{"name": name, "client": stringValue(b["client"]), "location": stringValue(b["location"]), "event_date": stringValue(b["event_date"]), "price": int64(math.Round(price * 100)), "status": "Draft", "archived": 0, "created": timestamp(), "watermark_text": "PICTIDE · PROOF", "watermark_style": "diagonal", "watermark_color": "#ffffff", "watermark_opacity": 48, "watermark_size": 5, "brand_position": "bottom-right", "brand_size": 18, "brand_opacity": 90, "share_title": "", "share_message": "", "share_color": "#4f5b2a", "share_qr_background": "#ffffff", "cover_photo": "", "allow_guest_upload": 0, "card_design": ""}
	if text := stringValue(b["watermark_text"]); text != "" && len(text) <= 200 {
		fields["watermark_text"] = text
	}
	if style := stringValue(b["watermark_style"]); style == "diagonal" || style == "center" || style == "tile" || style == "corner" {
		fields["watermark_style"] = style
	}
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	if err = store.Create(r.Context(), id, fields); err != nil {
		fail(w, 500, "Unable to create gallery.")
		return
	}
	fields["id"] = id
	fields["photo_count"] = 0
	reply(w, 201, fields)
}

func (s *Server) getGallery(w http.ResponseWriter, r *http.Request, user *account, id string) {
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	g, err := store.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			http.NotFound(w, r)
		} else {
			fail(w, 500, "Unable to load gallery.")
		}
		return
	}
	cur, err := s.db.Collection("photos").Find(r.Context(), bson.M{"studio_id": user.ID, "gallery_id": id, "ready": true}, options.Find().SetProjection(bson.M{"_id": 0, "studio_id": 0, "hash": 0, "ready": 0}).SetSort(bson.D{{Key: "created", Value: -1}}))
	if err != nil {
		fail(w, 500, "Unable to load photographs.")
		return
	}
	defer cur.Close(r.Context())
	photos := []bson.M{}
	if cur.All(r.Context(), &photos) != nil {
		fail(w, 500, "Unable to load photographs.")
		return
	}
	g["photos"] = photos
	g["photo_count"] = len(photos)
	var setting struct {
		StudioName   string `bson:"studio_name"`
		ContactEmail string `bson:"contact_email"`
	}
	if s.db.Collection("settings").FindOne(r.Context(), bson.M{"_id": user.ID}).Decode(&setting) != nil {
		setting.StudioName = "Pictide Studio"
	}
	g["studio_name"] = setting.StudioName
	g["contact_email"] = setting.ContactEmail
	reply(w, 200, g)
}

func (s *Server) updateGallery(w http.ResponseWriter, r *http.Request, user *account, id string) {
	var b map[string]any
	if decode(r, &b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	fields := bson.M{}
	for _, k := range []string{"name", "client", "location", "event_date", "watermark_text", "watermark_style"} {
		if v, ok := b[k]; ok {
			str, valid := v.(string)
			if !valid || len(str) > 200 || (k == "name" && strings.TrimSpace(str) == "") {
				fail(w, 400, "Invalid gallery details.")
				return
			}
			fields[k] = str
		}
	}
	if v, ok := b["price"]; ok {
		price, valid := number(v)
		if !valid || price < 0 || price > 10000000 {
			fail(w, 400, "Enter a valid price.")
			return
		}
		fields["price"] = int64(math.Round(price * 100))
	}
	if v, ok := b["status"]; ok {
		status, valid := v.(string)
		if !valid || (status != "Draft" && status != "Published") {
			fail(w, 400, "Invalid status.")
			return
		}
		fields["status"] = status
	}
	if len(fields) == 0 {
		fail(w, 400, "No gallery changes provided.")
		return
	}
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	if err := store.Update(r.Context(), id, fields); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			http.NotFound(w, r)
		} else {
			fail(w, 500, "Unable to update gallery.")
		}
		return
	}
	s.getGallery(w, r, user, id)
}
func (s *Server) archiveGallery(w http.ResponseWriter, r *http.Request, user *account, id string) {
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	if err := store.Archive(r.Context(), id); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			http.NotFound(w, r)
		} else {
			fail(w, 500, "Unable to remove gallery.")
		}
		return
	}
	reply(w, 200, bson.M{"ok": true})
}

type order struct {
	ID          string `bson:"id" json:"id"`
	GalleryID   string `bson:"gallery_id" json:"gallery_id"`
	Email       string `bson:"email" json:"email"`
	Status      string `bson:"status" json:"status"`
	Created     string `bson:"created" json:"created"`
	Amount      int64  `bson:"amount" json:"amount"`
	PlatformFee int64  `bson:"platform_fee" json:"platform_fee"`
}

func (s *Server) readOrders(r *http.Request, studio string) ([]order, error) {
	cur, err := s.db.Collection("orders").Find(r.Context(), bson.M{"studio_id": studio}, options.Find().SetSort(bson.D{{Key: "created", Value: -1}, {Key: "id", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(r.Context())
	rows := []order{}
	err = cur.All(r.Context(), &rows)
	return rows, err
}
func (s *Server) listOrders(w http.ResponseWriter, r *http.Request, user *account) {
	limit, offset, err := pagination(r)
	if err != nil {
		fail(w, 400, "Invalid pagination.")
		return
	}
	orders, err := s.readOrders(r, user.ID)
	if err != nil {
		fail(w, 500, "Unable to load orders.")
		return
	}
	items := []bson.M{}
	var paid, pending, gross, fees int64
	for _, o := range orders {
		if o.Status == "Paid" {
			paid++
			gross += o.Amount
			fees += o.PlatformFee
		}
		if o.Status == "Pending" {
			pending++
		}
	}
	for i, o := range orders {
		if int64(i) < offset || int64(i) >= offset+limit {
			continue
		}
		var gallery struct {
			Name string `bson:"name"`
		}
		if s.db.Collection("galleries").FindOne(r.Context(), bson.M{"studio_id": user.ID, "id": o.GalleryID}).Decode(&gallery) != nil {
			gallery.Name = "Removed gallery"
		}
		items = append(items, bson.M{"id": o.ID, "gallery_id": o.GalleryID, "gallery_name": gallery.Name, "email": o.Email, "status": o.Status, "created": o.Created, "amount": o.Amount, "platform_fee": o.PlatformFee})
	}
	average := int64(0)
	if paid > 0 {
		average = (gross + paid/2) / paid
	}
	reply(w, 200, bson.M{"items": items, "total": len(orders), "offset": offset, "limit": limit, "has_more": offset+int64(len(items)) < int64(len(orders)), "galleries": []any{}, "insights": bson.M{"paid": paid, "pending": pending, "gross": gross, "net": gross - fees, "average": average, "days": []any{}, "galleries": []any{}}})
}
