package httpapi

import (
	"encoding/json"
	"image/color"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"pictide.local/studio/internal/security"
)

// TestE2E_Tier4 exercises end-to-end real-world production lifecycle scenarios with multiple actors.
func TestE2E_Tier4(t *testing.T) {
	env := newE2EEnv(t, "t4")

	adminCookie := env.adminLogin(t)

	t.Run("Scenario_CompleteStudioLifecycle", func(t *testing.T) {
		// ==========================================
		// PHASE 1: Photographer Onboarding
		// ==========================================
		username := "studio_alpha"
		email := "alpha@studiomail.com"
		password := "StudioAlphaSecret2026!"

		// 1. Registration
		regBody, _ := json.Marshal(map[string]string{
			"username": username,
			"email":    email,
			"password": password,
		})
		wReg := env.request("POST", "/api/register", regBody, "application/json", nil)
		if wReg.Code == http.StatusServiceUnavailable && strings.Contains(wReg.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/register returned 503 stub: %s", wReg.Body.String())
		}
		if wReg.Code != http.StatusCreated && wReg.Code != http.StatusOK {
			t.Fatalf("registration failed: status %d body %s", wReg.Code, wReg.Body.String())
		}

		// Ensure verified in database for login
		_, _ = env.db.Collection("accounts").UpdateOne(env.ctx, bson.M{"username": username}, bson.M{"$set": bson.M{"email_verified": true}})

		// 2. Login
		photogCookie := env.login(t, username, password)
		if photogCookie == nil {
			t.Fatalf("photographer login failed")
		}

		// Look up studio ID
		var photogAccount struct {
			ID string `bson:"_id"`
		}
		_ = env.db.Collection("accounts").FindOne(env.ctx, bson.M{"username": username}).Decode(&photogAccount)
		studioID := photogAccount.ID

		// ==========================================
		// PHASE 2: Studio Branding & Configuration
		// ==========================================
		studioLogoPNG := createTestPNGBytes(100, 50, color.RGBA{R: 30, G: 30, B: 30, A: 255})
		wLogo := env.multipartRequest("POST", "/api/settings/logo", map[string][]testFile{"logo": {{"alpha_mark.png", studioLogoPNG}}}, nil, photogCookie)
		if wLogo.Code == http.StatusServiceUnavailable && strings.Contains(wLogo.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("studio logo returned 503 stub: %s", wLogo.Body.String())
		}

		settingsBody, _ := json.Marshal(map[string]any{
			"studio_name":   "Studio Alpha Films",
			"default_price": 30,
			"contact_email": "hello@studioalpha.com",
		})
		wSet := env.request("PUT", "/api/settings", settingsBody, "application/json", photogCookie)
		if wSet.Code != http.StatusOK {
			t.Fatalf("save settings failed: status %d body %s", wSet.Code, wSet.Body.String())
		}

		// ==========================================
		// PHASE 3: Gallery Creation & Customization
		// ==========================================
		galID := env.createGallery(t, photogCookie, "Alice and Bob Wedding", 3000)

		// Upload professional photos
		photo1ID, _ := security.Token()
		photo2ID, _ := security.Token()
		now := time.Now().UTC()
		_, _ = env.db.Collection("photos").InsertMany(env.ctx, []any{
			bson.M{
				"id":         photo1ID,
				"studio_id":  studioID,
				"gallery_id": galID,
				"name":       "Ceremony_001.jpg",
				"hash":       "photo1_hash_alpha",
				"ready":      true,
				"price":      3000,
				"archived":   0,
				"created":    now.Add(-3 * time.Hour).Format(time.RFC3339Nano),
			},
			bson.M{
				"id":         photo2ID,
				"studio_id":  studioID,
				"gallery_id": galID,
				"name":       "Reception_042.jpg",
				"hash":       "photo2_hash_alpha",
				"ready":      true,
				"price":      3000,
				"archived":   0,
				"created":    now.Add(-2 * time.Hour).Format(time.RFC3339Nano),
			},
		})

		// Configure gallery: watermark, QR branding, enable guest uploads
		customBody, _ := json.Marshal(map[string]any{
			"watermark_text":     "Studio Alpha",
			"watermark_style":    "diagonal",
			"watermark_color":    "#ffffff",
			"watermark_opacity":  35,
			"watermark_size":     6,
			"share_title":        "Alice & Bob's Wedding Celebration",
			"share_message":      "Thank you for celebrating with us!",
			"share_color":        "#2b4c7e",
			"allow_guest_upload": 1,
		})
		wCust := env.request("PATCH", "/api/galleries/"+galID+"/customization", customBody, "application/json", photogCookie)
		if wCust.Code == http.StatusServiceUnavailable && strings.Contains(wCust.Body.String(), "Guest uploads are not available yet.") {
			t.Fatalf("customization returned 503 stub: %s", wCust.Body.String())
		}

		// Publish gallery
		_, _ = env.db.Collection("galleries").UpdateOne(env.ctx, bson.M{"id": galID}, bson.M{"$set": bson.M{"status": "Published"}})

		// ==========================================
		// PHASE 4: Guest Interactions & Submissions
		// ==========================================
		// Public visitors view gallery
		wPub := env.request("GET", "/api/galleries/"+galID, nil, "", nil)
		if wPub.Code != http.StatusOK {
			t.Fatalf("public gallery view failed: status %d body %s", wPub.Code, wPub.Body.String())
		}

		// Guest submits photo 1
		g1PNG := createTestPNGBytes(90, 90, color.RGBA{R: 200, G: 150, B: 50, A: 255})
		wG1 := env.multipartRequest("POST", "/api/galleries/"+galID+"/guest-uploads", map[string][]testFile{"photos": {{"guest_toast.png", g1PNG}}}, nil, nil)
		if wG1.Code == http.StatusServiceUnavailable && strings.Contains(wG1.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("guest upload returned 503 stub: %s", wG1.Body.String())
		}

		var g1Res struct {
			Token string `json:"token"`
			Added []struct {
				ID string `json:"id"`
			} `json:"added"`
		}
		_ = json.Unmarshal(wG1.Body.Bytes(), &g1Res)

		// If guest photo was uploaded, photographer reviews and approves it
		if len(g1Res.Added) > 0 {
			guestPhotoID := g1Res.Added[0].ID
			approveBody, _ := json.Marshal(map[string]string{"action": "approve"})
			wApprove := env.request("POST", "/api/guest-uploads/"+guestPhotoID+"/review", approveBody, "application/json", photogCookie)
			if wApprove.Code == http.StatusServiceUnavailable && strings.Contains(wApprove.Body.String(), "This feature is still being converted to MongoDB.") {
				t.Fatalf("guest review returned 503 stub: %s", wApprove.Body.String())
			}
		}

		// ==========================================
		// PHASE 5: Customer Checkout & Payment
		// ==========================================
		checkoutBody, _ := json.Marshal(map[string]any{
			"gallery_id": galID,
			"email":      "client_guest@example.com",
			"photos":     []string{photo1ID},
		})
		wOrder := env.request("POST", "/api/checkout", checkoutBody, "application/json", nil)
		if wOrder.Code != http.StatusOK {
			t.Fatalf("checkout failed: status %d body %s", wOrder.Code, wOrder.Body.String())
		}
		var oRes struct {
			Order string `json:"order"`
		}
		_ = json.Unmarshal(wOrder.Body.Bytes(), &oRes)
		orderID := oRes.Order

		// Paystack webhook confirms payment
		chargePayload, _ := json.Marshal(map[string]any{
			"event": "charge.success",
			"data": map[string]any{
				"reference": orderID,
				"status":    "success",
				"amount":    3000,
				"currency":  "NGN",
			},
		})
		sig := computePaystackSignature(env.cfg.PaystackSecret, chargePayload)
		wHook := env.requestWithHeader("POST", "/api/v1/webhooks/paystack", chargePayload, map[string]string{
			"Content-Type":         "application/json",
			"X-Paystack-Signature": sig,
		}, nil)
		if wHook.Code != http.StatusOK {
			t.Fatalf("webhook charge.success failed: status %d body %s", wHook.Code, wHook.Body.String())
		}

		// ==========================================
		// PHASE 6: Orders & Analytics Reporting
		// ==========================================
		wOrders := env.request("GET", "/api/orders?status=Paid", nil, "", photogCookie)
		if wOrders.Code != http.StatusOK {
			t.Fatalf("GET /api/orders failed: status %d body %s", wOrders.Code, wOrders.Body.String())
		}

		// ==========================================
		// PHASE 7: Payout & Withdrawal
		// ==========================================
		wWallet := env.request("GET", "/api/wallet", nil, "", photogCookie)
		if wWallet.Code != http.StatusOK {
			t.Fatalf("GET /api/wallet failed: status %d body %s", wWallet.Code, wWallet.Body.String())
		}

		// Payout request
		withdrawalRef := "alpha_payout_ref_001"
		_, _ = env.db.Collection("withdrawals").InsertOne(env.ctx, bson.M{
			"id":            "alpha_w_1",
			"studio_id":     studioID,
			"reference":     withdrawalRef,
			"amount":        int64(2500),
			"status":        "pending",
			"account_last4": "9999",
			"created":       time.Now().UTC().Format(time.RFC3339Nano),
		})

		transferPayload, _ := json.Marshal(map[string]any{
			"event": "transfer.success",
			"data": map[string]any{
				"reference": withdrawalRef,
				"status":    "success",
				"amount":    2500,
				"currency":  "NGN",
			},
		})
		sigXfer := computePaystackSignature(env.cfg.PaystackSecret, transferPayload)
		wXfer := env.requestWithHeader("POST", "/api/v1/webhooks/paystack", transferPayload, map[string]string{
			"Content-Type":         "application/json",
			"X-Paystack-Signature": sigXfer,
		}, nil)
		if wXfer.Code == http.StatusServiceUnavailable {
			t.Fatalf("transfer webhook returned 503 stub: %s", wXfer.Body.String())
		}

		// ==========================================
		// PHASE 8: Administrator Audit & Governance
		// ==========================================
		wAdminOver := env.request("GET", "/api/admin/overview", nil, "", adminCookie)
		if wAdminOver.Code != http.StatusOK {
			t.Fatalf("admin overview failed: status %d body %s", wAdminOver.Code, wAdminOver.Body.String())
		}

		// Admin sync record
		syncBody, _ := json.Marshal(map[string]any{
			"accountId": studioID,
			"reference": orderID,
			"kind":      "order",
		})
		wSync := env.request("POST", "/api/admin/sync", syncBody, "application/json", adminCookie)
		if wSync.Code == http.StatusServiceUnavailable && strings.Contains(wSync.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/admin/sync returned 503 stub: %s", wSync.Body.String())
		}
	})
}
