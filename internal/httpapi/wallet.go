package httpapi

import (
	"net/http"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// wallet reports recorded balances; payout creation remains unavailable.
func (s *Server) wallet(w http.ResponseWriter, r *http.Request, user *account) {
	ctx := r.Context()
	orders, err := s.db.Collection("orders").Find(ctx, bson.M{"studio_id": user.ID, "status": "Paid", "payment_provider": "paystack"}, options.Find().SetProjection(bson.M{"amount": 1, "platform_fee": 1}))
	if err != nil {
		fail(w, 500, "Could not load wallet.")
		return
	}
	defer orders.Close(ctx)
	var earned int64
	for orders.Next(ctx) {
		var order struct {
			Amount int64 `bson:"amount"`
			Fee    int64 `bson:"platform_fee"`
		}
		if orders.Decode(&order) != nil {
			fail(w, 500, "Could not load wallet.")
			return
		}
		earned += order.Amount - order.Fee
	}
	if orders.Err() != nil {
		fail(w, 500, "Could not load wallet.")
		return
	}
	cur, err := s.db.Collection("withdrawals").Find(ctx, bson.M{"studio_id": user.ID}, options.Find().SetSort(bson.D{{Key: "created", Value: -1}}))
	if err != nil {
		fail(w, 500, "Could not load wallet.")
		return
	}
	defer cur.Close(ctx)
	list := []bson.M{}
	var held int64
	for cur.Next(ctx) {
		var v struct {
			Reference    string `bson:"reference"`
			AccountLast4 string `bson:"account_last4"`
			Status       string `bson:"status"`
			Created      string `bson:"created"`
			Amount       int64  `bson:"amount"`
		}
		if cur.Decode(&v) != nil {
			fail(w, 500, "Could not load wallet.")
			return
		}
		if v.Status != "failed" && v.Status != "reversed" {
			held += v.Amount
		}
		list = append(list, bson.M{"reference": v.Reference, "account_last4": v.AccountLast4, "status": v.Status, "created": v.Created, "amount": v.Amount})
	}
	if cur.Err() != nil {
		fail(w, 500, "Could not load wallet.")
		return
	}
	available := earned - held
	if available < 0 {
		available = 0
	}
	reply(w, 200, bson.M{"earned": earned, "available": available, "feeBps": s.cfg.FeeBPS, "withdrawals": list, "configured": false})
}
