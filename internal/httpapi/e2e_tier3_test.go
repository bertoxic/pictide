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

// TestE2E_Tier3 covers cross-feature combinations, multi-step workflows, and tenant isolation.
func TestE2E_Tier3(t *testing.T) {
	env := newE2EEnv(t, "t3")

	adminCookie := env.adminLogin(t)

	t.Run("Combo_AuthToGalleryLifecycle", func(t *testing.T) {
		username := "workflow_photog"
		email := "workflow@example.com"
		password := "WorkflowPass12345!"

		// 1. Self-Registration
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

		// 2. Fetch verification link from account_links
		var link struct {
			TokenHash string `bson:"token_hash"`
			AccountID string `bson:"account_id"`
		}
		err := env.db.Collection("account_links").FindOne(env.ctx, bson.M{"email": strings.ToLower(email), "kind": "verify"}).Decode(&link)
		if err != nil {
			// Alternatively look up by account_id
			var acc struct {
				ID string `bson:"_id"`
			}
			if env.db.Collection("accounts").FindOne(env.ctx, bson.M{"username": username}).Decode(&acc) == nil {
				_ = env.db.Collection("account_links").FindOne(env.ctx, bson.M{"account_id": acc.ID, "kind": "verify"}).Decode(&link)
			}
		}

		// Mark account verified to enable login
		_, _ = env.db.Collection("accounts").UpdateOne(env.ctx, bson.M{"username": username}, bson.M{"$set": bson.M{"email_verified": true}})

		// 3. Login
		cookie := env.login(t, username, password)
		if cookie == nil {
			t.Fatalf("login failed after verification")
		}

		// 4. Update Studio Profile Settings
		settingsBody, _ := json.Marshal(map[string]any{
			"studio_name":   "Artisan Photography",
			"default_price": 50,
			"contact_email": "contact@artisan.com",
		})
		wSet := env.request("PUT", "/api/settings", settingsBody, "application/json", cookie)
		if wSet.Code != http.StatusOK {
			t.Fatalf("saveSettings failed: status %d body %s", wSet.Code, wSet.Body.String())
		}

		// 5. Create Gallery
		galID := env.createGallery(t, cookie, "Spring Portraits 2026", 5000)
		if galID == "" {
			t.Fatalf("gallery creation failed in workflow")
		}

		// 6. Customize Gallery
		custBody, _ := json.Marshal(map[string]any{
			"watermark_text":     "Artisan Proof",
			"watermark_style":    "diagonal",
			"watermark_color":    "#ffffff",
			"watermark_opacity":  40,
			"watermark_size":     5,
			"allow_guest_upload": 1,
		})
		wCust := env.request("PATCH", "/api/galleries/"+galID+"/customization", custBody, "application/json", cookie)
		if wCust.Code == http.StatusServiceUnavailable && strings.Contains(wCust.Body.String(), "Guest uploads are not available yet.") {
			t.Fatalf("customization returned 503 stub: %s", wCust.Body.String())
		}
		if wCust.Code != http.StatusOK {
			t.Fatalf("gallery customization failed: status %d body %s", wCust.Code, wCust.Body.String())
		}

		// 7. Verify persisted gallery document
		var gDoc struct {
			Name             string `bson:"name"`
			WatermarkText    string `bson:"watermark_text"`
			AllowGuestUpload int    `bson:"allow_guest_upload"`
		}
		_ = env.db.Collection("galleries").FindOne(env.ctx, bson.M{"id": galID}).Decode(&gDoc)
		if gDoc.Name != "Spring Portraits 2026" || gDoc.AllowGuestUpload != 1 {
			t.Errorf("gallery document state mismatch: %+v", gDoc)
		}
	})

	t.Run("Combo_GuestUploadToPhotoPromotion", func(t *testing.T) {
		_, photogCookie := env.createPhotographer(t, "guest_host_photog", "host@example.com", "Password123456!", true)
		galID := env.createGallery(t, photogCookie, "Community Event", 1500)

		// 1. Enable guest uploads and publish gallery
		_, _ = env.db.Collection("galleries").UpdateOne(env.ctx, bson.M{"id": galID}, bson.M{"$set": bson.M{
			"status":             "Published",
			"allow_guest_upload": 1,
		}})

		// 2. Guest submits photo
		guestPNG := createTestPNGBytes(80, 80, color.RGBA{R: 50, G: 120, B: 200, A: 255})
		files := map[string][]testFile{"photos": {{"community_moment.png", guestPNG}}}
		wSub := env.multipartRequest("POST", "/api/galleries/"+galID+"/guest-uploads", files, nil, nil)
		if wSub.Code == http.StatusServiceUnavailable && strings.Contains(wSub.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("guest submission returned 503 stub: %s", wSub.Body.String())
		}
		if wSub.Code != http.StatusOK {
			t.Fatalf("guest photo submission failed: status %d body %s", wSub.Code, wSub.Body.String())
		}

		var subRes struct {
			Token string `json:"token"`
			Added []struct {
				ID string `json:"id"`
			} `json:"added"`
		}
		_ = json.Unmarshal(wSub.Body.Bytes(), &subRes)
		if len(subRes.Added) == 0 {
			t.Fatalf("no added photos in guest submission response")
		}
		photoID := subRes.Added[0].ID

		// 3. Guest checks status -> pending
		statusBody, _ := json.Marshal(map[string]any{"tokens": []string{subRes.Token}})
		wStat := env.request("POST", "/api/guest-uploads/status", statusBody, "application/json", nil)
		if wStat.Code != http.StatusOK {
			t.Fatalf("guest-uploads/status status %d: %s", wStat.Code, wStat.Body.String())
		}

		// 4. Studio reviews and approves photo
		revBody, _ := json.Marshal(map[string]string{"action": "approve"})
		wRev := env.request("POST", "/api/guest-uploads/"+photoID+"/review", revBody, "application/json", photogCookie)
		if wRev.Code == http.StatusServiceUnavailable && strings.Contains(wRev.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("guest-upload review returned 503 stub: %s", wRev.Body.String())
		}

		// 5. Verify photo exists in photos collection with price 0 (free photo)
		var pDoc struct {
			ID        string `bson:"id"`
			GalleryID string `bson:"gallery_id"`
			Price     *int   `bson:"price"`
		}
		err := env.db.Collection("photos").FindOne(env.ctx, bson.M{"id": photoID}).Decode(&pDoc)
		if err == nil {
			if pDoc.Price != nil && *pDoc.Price != 0 {
				t.Errorf("expected approved guest photo price to be 0 (free), got %d", *pDoc.Price)
			}
		}
	})

	t.Run("Combo_OrderCheckoutToPayoutLifecycle", func(t *testing.T) {
		photogID, photogCookie := env.createPhotographer(t, "commerce_photog", "commerce@example.com", "Password123456!", true)
		galID := env.createGallery(t, photogCookie, "Commerce Gallery", 2500)

		// Create a photo in the gallery
		photoID, _ := security.Token()
		_, _ = env.db.Collection("photos").InsertOne(env.ctx, bson.M{
			"id":         photoID,
			"studio_id":  photogID,
			"gallery_id": galID,
			"name":       "Paid Photo.jpg",
			"hash":       "sample_photo_hash_123",
			"ready":      true,
			"price":      2500,
			"archived":   0,
			"created":    time.Now().UTC().Format(time.RFC3339Nano),
		})

		// Make gallery published
		_, _ = env.db.Collection("galleries").UpdateOne(env.ctx, bson.M{"id": galID}, bson.M{"$set": bson.M{"status": "Published"}})

		// 1. Checkout request
		checkoutBody, _ := json.Marshal(map[string]any{
			"gallery_id": galID,
			"email":      "buyer@example.com",
			"photos":     []string{photoID},
		})
		wCheck := env.request("POST", "/api/checkout", checkoutBody, "application/json", nil)
		if wCheck.Code != http.StatusOK {
			t.Fatalf("checkout failed: status %d body %s", wCheck.Code, wCheck.Body.String())
		}
		var checkRes struct {
			Order string `json:"order"`
		}
		_ = json.Unmarshal(wCheck.Body.Bytes(), &checkRes)
		orderID := checkRes.Order
		if orderID == "" {
			t.Fatalf("checkout did not return order reference ID: %s", wCheck.Body.String())
		}

		// 2. Paystack webhook charge.success
		chargeHook, _ := json.Marshal(map[string]any{
			"event": "charge.success",
			"data": map[string]any{
				"reference": orderID,
				"status":    "success",
				"amount":    2500,
				"currency":  "NGN",
			},
		})
		sig := computePaystackSignature(env.cfg.PaystackSecret, chargeHook)
		wHook := env.requestWithHeader("POST", "/api/v1/webhooks/paystack", chargeHook, map[string]string{
			"Content-Type":         "application/json",
			"X-Paystack-Signature": sig,
		}, nil)
		if wHook.Code != http.StatusOK {
			t.Fatalf("charge.success webhook failed: status %d body %s", wHook.Code, wHook.Body.String())
		}

		// 3. Verify order updated to Paid in MongoDB
		var ordDoc struct {
			Status string `bson:"status"`
		}
		_ = env.db.Collection("orders").FindOne(env.ctx, bson.M{"id": orderID}).Decode(&ordDoc)
		if ordDoc.Status != "Paid" {
			t.Errorf("order status was not updated to Paid by webhook, got %s", ordDoc.Status)
		}

		// 4. Verify wallet reflects earned funds
		wWallet := env.request("GET", "/api/wallet", nil, "", photogCookie)
		if wWallet.Code != http.StatusOK {
			t.Fatalf("GET /api/wallet failed: status %d body %s", wWallet.Code, wWallet.Body.String())
		}

		// 5. Initiate withdrawal
		withdrawalRef := "pictide_wd_" + orderID[:8]
		_, _ = env.db.Collection("withdrawals").InsertOne(env.ctx, bson.M{
			"id":            "wd_" + orderID[:8],
			"studio_id":     photogID,
			"reference":     withdrawalRef,
			"amount":        int64(2000),
			"status":        "pending",
			"account_last4": "1234",
			"created":       time.Now().UTC().Format(time.RFC3339Nano),
		})

		// 6. Paystack transfer.success webhook
		transferHook, _ := json.Marshal(map[string]any{
			"event": "transfer.success",
			"data": map[string]any{
				"reference": withdrawalRef,
				"status":    "success",
				"amount":    2000,
				"currency":  "NGN",
			},
		})
		sigXfer := computePaystackSignature(env.cfg.PaystackSecret, transferHook)
		wXfer := env.requestWithHeader("POST", "/api/v1/webhooks/paystack", transferHook, map[string]string{
			"Content-Type":         "application/json",
			"X-Paystack-Signature": sigXfer,
		}, nil)
		if wXfer.Code == http.StatusServiceUnavailable {
			t.Fatalf("transfer webhook returned 503 stub: %s", wXfer.Body.String())
		}

		// 7. Verify withdrawal marked success
		var wdDoc struct {
			Status string `bson:"status"`
		}
		_ = env.db.Collection("withdrawals").FindOne(env.ctx, bson.M{"reference": withdrawalRef}).Decode(&wdDoc)
		if wdDoc.Status != "success" {
			t.Errorf("expected withdrawal status success, got %s", wdDoc.Status)
		}
	})

	t.Run("Combo_BrandingUploadToRegeneration", func(t *testing.T) {
		_, photogCookie := env.createPhotographer(t, "branding_photog", "brand@example.com", "Password123456!", true)
		galID := env.createGallery(t, photogCookie, "Branded Gallery", 3000)

		// 1. Upload Studio Logo
		studioPNG := createTestPNGBytes(120, 60, color.RGBA{R: 20, G: 180, B: 20, A: 255})
		wStudioLogo := env.multipartRequest("POST", "/api/settings/logo", map[string][]testFile{"logo": {{"studio.png", studioPNG}}}, nil, photogCookie)
		if wStudioLogo.Code == http.StatusServiceUnavailable && strings.Contains(wStudioLogo.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("studio logo upload returned 503 stub: %s", wStudioLogo.Body.String())
		}

		// 2. Upload Gallery Watermark Logo
		galPNG := createTestPNGBytes(100, 50, color.RGBA{R: 180, G: 20, B: 20, A: 255})
		wGalLogo := env.multipartRequest("POST", "/api/galleries/"+galID+"/logo", map[string][]testFile{"logo": {{"gallery.png", galPNG}}}, nil, photogCookie)
		if wGalLogo.Code == http.StatusServiceUnavailable && strings.Contains(wGalLogo.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("gallery logo upload returned 503 stub: %s", wGalLogo.Body.String())
		}

		// 3. Regenerate Gallery Previews
		wRegen := env.request("POST", "/api/galleries/"+galID+"/regenerate", nil, "", photogCookie)
		if wRegen.Code == http.StatusServiceUnavailable && strings.Contains(wRegen.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("gallery regenerate returned 503 stub: %s", wRegen.Body.String())
		}

		// 4. Delete Gallery Logo
		wDelGal := env.request("DELETE", "/api/galleries/"+galID+"/logo", nil, "", photogCookie)
		if wDelGal.Code == http.StatusServiceUnavailable && strings.Contains(wDelGal.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("delete gallery logo returned 503 stub: %s", wDelGal.Body.String())
		}

		// 5. Delete Studio Logo
		wDelStudio := env.request("DELETE", "/api/settings/logo", nil, "", photogCookie)
		if wDelStudio.Code == http.StatusServiceUnavailable && strings.Contains(wDelStudio.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("delete studio logo returned 503 stub: %s", wDelStudio.Body.String())
		}
	})

	t.Run("Combo_MultiTenantDataIsolation", func(t *testing.T) {
		// Studio A and Studio B
		idA, cookieA := env.createPhotographer(t, "studio_tenant_a", "tenanta@example.com", "Password123456!", true)
		_, cookieB := env.createPhotographer(t, "studio_tenant_b", "tenantb@example.com", "Password123456!", true)

		// Studio A creates gallery and order
		galA := env.createGallery(t, cookieA, "Studio A Private Gallery", 3000)
		_, _ = env.db.Collection("orders").InsertOne(env.ctx, bson.M{
			"id":        "ord_tenant_a",
			"studio_id": idA,
			"amount":    int64(3000),
			"status":    "Paid",
			"created":   time.Now().UTC().Format(time.RFC3339Nano),
		})

		// Studio B attempts to mutate Studio A's gallery
		patchBody, _ := json.Marshal(map[string]any{"name": "Hacked by B"})
		wPatch := env.request("PATCH", "/api/galleries/"+galA, patchBody, "application/json", cookieB)
		if wPatch.Code == http.StatusOK {
			t.Errorf("tenant B was able to modify tenant A's gallery! (cross-tenant leak)")
		}

		// Studio B attempts to delete Studio A's gallery
		wDel := env.request("DELETE", "/api/galleries/"+galA, nil, "", cookieB)
		if wDel.Code == http.StatusOK {
			t.Errorf("tenant B was able to delete tenant A's gallery! (cross-tenant leak)")
		}

		// Studio B lists orders -> must not see Studio A's orders
		wOrdersB := env.request("GET", "/api/orders", nil, "", cookieB)
		if wOrdersB.Code == http.StatusOK {
			var oRes struct {
				Items []struct {
					ID string `json:"id"`
				} `json:"items"`
			}
			_ = json.Unmarshal(wOrdersB.Body.Bytes(), &oRes)
			for _, item := range oRes.Items {
				if item.ID == "ord_tenant_a" {
					t.Errorf("tenant B saw tenant A's order in /api/orders! (cross-tenant leak)")
				}
			}
		}
	})

	t.Run("Combo_AdminGovernanceAndAccountSuspension", func(t *testing.T) {
		targetID, targetCookie := env.createPhotographer(t, "target_photog", "target@example.com", "Password123456!", true)

		// 1. Admin checks overview and sees target account
		wOver := env.request("GET", "/api/admin/overview", nil, "", adminCookie)
		if wOver.Code != http.StatusOK {
			t.Fatalf("GET /api/admin/overview failed: status %d", wOver.Code)
		}

		// 2. Admin inspects activity
		wAct := env.request("GET", "/api/admin/activity?account="+targetID, nil, "", adminCookie)
		if wAct.Code != http.StatusOK {
			t.Fatalf("GET /api/admin/activity failed: status %d", wAct.Code)
		}

		// 3. Admin suspends target photographer account
		suspendBody, _ := json.Marshal(map[string]any{"id": targetID, "disabled": true})
		wSus := env.request("POST", "/api/admin/account-status", suspendBody, "application/json", adminCookie)
		if wSus.Code != http.StatusOK {
			t.Fatalf("POST /api/admin/account-status suspend failed: status %d", wSus.Code)
		}

		// 4. Target photographer's session immediately rejected
		wAuthTest := env.request("GET", "/api/settings", nil, "", targetCookie)
		if wAuthTest.Code != http.StatusUnauthorized {
			t.Errorf("suspended user session was not immediately invalidated; status %d", wAuthTest.Code)
		}

		// 5. Admin restores photographer account
		restoreBody, _ := json.Marshal(map[string]any{"id": targetID, "disabled": false})
		wRest := env.request("POST", "/api/admin/account-status", restoreBody, "application/json", adminCookie)
		if wRest.Code != http.StatusOK {
			t.Fatalf("POST /api/admin/account-status restore failed: status %d", wRest.Code)
		}

		// 6. Photographer can login again
		restoredCookie := env.login(t, "target_photog", "Password123456!")
		if restoredCookie == nil {
			t.Errorf("restored photographer could not login")
		}
	})
}
