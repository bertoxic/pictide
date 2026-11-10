package httpapi

import (
	"net/http"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

func (s *Server) adminInsights(w http.ResponseWriter, r *http.Request, user *account) {
	if user.Role != "admin" {
		fail(w, 403, "Administrator access required.")
		return
	}
	ctx := r.Context()
	totals := map[string]int64{"paid_orders": 0, "gross": 0, "platform_fees": 0, "photographer_earnings": 0, "held_withdrawals": 0, "completed_withdrawals": 0, "pending_orders": 0, "overdue_orders": 0, "pending_contributions": 0, "published_galleries": 0}
	var err error
	totals["published_galleries"], err = s.db.Collection("galleries").CountDocuments(ctx, bson.M{"status": "Published", "archived": bson.M{"$ne": 1}})
	if err != nil {
		fail(w, 500, "Unable to load platform analytics.")
		return
	}
	totals["pending_contributions"], err = s.db.Collection("guest_uploads").CountDocuments(ctx, bson.M{"status": "pending"})
	if err != nil {
		fail(w, 500, "Unable to load platform analytics.")
		return
	}
	names := map[string]string{}
	users, err := s.db.Collection("accounts").Find(ctx, bson.M{})
	if err != nil {
		fail(w, 500, "Unable to load platform analytics.")
		return
	}
	for users.Next(ctx) {
		var a account
		if users.Decode(&a) != nil {
			users.Close(ctx)
			fail(w, 500, "Unable to load platform analytics.")
			return
		}
		names[a.ID] = a.Username
	}
	err = users.Err()
	users.Close(ctx)
	if err != nil {
		fail(w, 500, "Unable to load platform analytics.")
		return
	}
	cur, err := s.db.Collection("orders").Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "created", Value: -1}}))
	if err != nil {
		fail(w, 500, "Unable to load platform analytics.")
		return
	}
	defer cur.Close(ctx)
	now := time.Now().UTC()
	days := map[string]bson.M{}
	recent := []bson.M{}
	for cur.Next(ctx) {
		var o purchase
		if cur.Decode(&o) != nil {
			fail(w, 500, "Unable to load platform analytics.")
			return
		}
		if o.Status == "Paid" && o.PaymentProvider == "paystack" {
			totals["paid_orders"]++
			totals["gross"] += o.Amount
			totals["platform_fees"] += o.PlatformFee
			day := ""
			if len(o.Created) >= 10 {
				day = o.Created[:10]
			}
			if day != "" {
				if days[day] == nil {
					days[day] = bson.M{"count": int64(0), "amount": int64(0)}
				}
				days[day]["count"] = days[day]["count"].(int64) + 1
				days[day]["amount"] = days[day]["amount"].(int64) + o.Amount
			}
		}
		if o.Status == "Pending" {
			totals["pending_orders"]++
			if created, e := time.Parse(time.RFC3339Nano, o.Created); e == nil && created.Before(now.Add(-24*time.Hour)) {
				totals["overdue_orders"]++
			}
		}
		if len(recent) < 200 {
			var g struct {
				Name string `bson:"name"`
			}
			if s.db.Collection("galleries").FindOne(ctx, bson.M{"id": o.GalleryID, "studio_id": o.StudioID}).Decode(&g) != nil {
				g.Name = "Removed gallery"
			}
			recent = append(recent, bson.M{"id": o.ID, "account_id": o.StudioID, "account": names[o.StudioID], "gallery": g.Name, "amount": o.Amount, "status": o.Status, "created": o.Created})
		}
	}
	if cur.Err() != nil {
		fail(w, 500, "Unable to load platform analytics.")
		return
	}
	totals["photographer_earnings"] = totals["gross"] - totals["platform_fees"]
	withdrawals := []bson.M{}
	wc, err := s.db.Collection("withdrawals").Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "created", Value: -1}}))
	if err != nil {
		fail(w, 500, "Unable to load platform analytics.")
		return
	}
	defer wc.Close(ctx)
	for wc.Next(ctx) {
		var v struct {
			StudioID  string `bson:"studio_id"`
			Reference string `bson:"reference"`
			Status    string `bson:"status"`
			Created   string `bson:"created"`
			Last4     string `bson:"account_last4"`
			Amount    int64  `bson:"amount"`
		}
		if wc.Decode(&v) != nil {
			fail(w, 500, "Unable to load platform analytics.")
			return
		}
		if v.Status == "pending" || v.Status == "otp" {
			totals["held_withdrawals"] += v.Amount
		}
		if v.Status == "success" {
			totals["completed_withdrawals"] += v.Amount
		}
		if len(withdrawals) < 200 {
			withdrawals = append(withdrawals, bson.M{"reference": v.Reference, "account_id": v.StudioID, "account": names[v.StudioID], "amount": v.Amount, "status": v.Status, "created": v.Created, "last4": v.Last4})
		}
	}
	if wc.Err() != nil {
		fail(w, 500, "Unable to load platform analytics.")
		return
	}
	timeline := make([]bson.M, 0, 90)
	for i := 89; i >= 0; i-- {
		day := now.AddDate(0, 0, -i).Format("2006-01-02")
		v := days[day]
		if v == nil {
			v = bson.M{"count": int64(0), "amount": int64(0)}
		}
		timeline = append(timeline, bson.M{"date": day, "count": v["count"], "amount": v["amount"]})
	}
	reply(w, 200, bson.M{"totals": totals, "timeline": timeline, "orders": recent, "withdrawals": withdrawals, "limit": 200})
}
