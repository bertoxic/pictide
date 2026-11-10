package httpapi

import (
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
	"pictide.local/studio/internal/security"
)

func (s *Server) listAccounts(w http.ResponseWriter, r *http.Request, user *account) {
	if user.Role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	cur, err := s.db.Collection("accounts").Find(r.Context(), bson.M{}, options.Find().SetSort(bson.D{{Key: "created", Value: 1}}))
	if err != nil {
		fail(w, 500, "Unable to load accounts.")
		return
	}
	defer cur.Close(r.Context())
	list := []bson.M{}
	for cur.Next(r.Context()) {
		var a account
		if cur.Decode(&a) != nil {
			fail(w, 500, "Unable to load accounts.")
			return
		}
		list = append(list, bson.M{"id": a.ID, "username": a.Username, "role": a.Role, "created": a.Created})
	}
	if cur.Err() != nil {
		fail(w, 500, "Unable to load accounts.")
		return
	}
	reply(w, 200, list)
}

func (s *Server) accountActivity(w http.ResponseWriter, r *http.Request, user *account) {
	if user.Role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	id := r.URL.Query().Get("account")
	var target account
	if id == "" || s.db.Collection("accounts").FindOne(r.Context(), bson.M{"_id": id}).Decode(&target) != nil {
		http.NotFound(w, r)
		return
	}
	auditID, err := security.Token()
	if err != nil {
		fail(w, 500, "Unable to record access.")
		return
	}
	if _, err = s.db.Collection("admin_audit").InsertOne(r.Context(), bson.M{"id": auditID, "actor": user.Username, "action": "viewed account activity", "target": target.Username, "created": timestamp()}); err != nil {
		fail(w, 500, "Unable to record access.")
		return
	}
	orders := []bson.M{}
	cur, err := s.db.Collection("orders").Find(r.Context(), bson.M{"studio_id": id}, options.Find().SetSort(bson.D{{Key: "created", Value: -1}}).SetLimit(100))
	if err != nil {
		fail(w, 500, "Unable to load activity.")
		return
	}
	for cur.Next(r.Context()) {
		var o purchase
		if cur.Decode(&o) != nil {
			cur.Close(r.Context())
			fail(w, 500, "Unable to load activity.")
			return
		}
		var gallery struct {
			Name string `bson:"name"`
		}
		if s.db.Collection("galleries").FindOne(r.Context(), bson.M{"id": o.GalleryID, "studio_id": id}).Decode(&gallery) != nil {
			gallery.Name = "Removed gallery"
		}
		orders = append(orders, bson.M{"id": o.ID, "gallery": gallery.Name, "email": o.Email, "amount": o.Amount, "status": o.Status, "created": o.Created})
	}
	err = cur.Err()
	cur.Close(r.Context())
	if err != nil {
		fail(w, 500, "Unable to load activity.")
		return
	}
	withdrawals := []bson.M{}
	wc, err := s.db.Collection("withdrawals").Find(r.Context(), bson.M{"studio_id": id}, options.Find().SetSort(bson.D{{Key: "created", Value: -1}}).SetLimit(100))
	if err != nil {
		fail(w, 500, "Unable to load activity.")
		return
	}
	for wc.Next(r.Context()) {
		var v struct {
			Reference string `bson:"reference"`
			Bank      string `bson:"bank"`
			Last4     string `bson:"account_last4"`
			Amount    int64  `bson:"amount"`
			Status    string `bson:"status"`
			Created   string `bson:"created"`
		}
		if wc.Decode(&v) != nil {
			wc.Close(r.Context())
			fail(w, 500, "Unable to load activity.")
			return
		}
		withdrawals = append(withdrawals, bson.M{"reference": v.Reference, "bank": v.Bank, "last4": v.Last4, "amount": v.Amount, "status": v.Status, "created": v.Created})
	}
	err = wc.Err()
	wc.Close(r.Context())
	if err != nil {
		fail(w, 500, "Unable to load activity.")
		return
	}
	reply(w, 200, bson.M{"username": target.Username, "orders": orders, "withdrawals": withdrawals, "limit": 100})
}
