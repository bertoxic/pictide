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

// TestE2E_Tier1 covers representative tests for all 30 features from PROJECT.md § Feature Inventory.
func TestE2E_Tier1(t *testing.T) {
	env := newE2EEnv(t, "t1")

	// Pre-create photographer account and admin session
	adminCookie := env.adminLogin(t)
	photogID, photogCookie := env.createPhotographer(t, "photog_t1", "photog_t1@example.com", "securepass123456", true)

	// Pre-create a gallery
	galleryID := env.createGallery(t, photogCookie, "Tier1 Gallery", 3500)

	t.Run("Feature01_PublicAndAuthenticatedRoutingDispatcher", func(t *testing.T) {
		// Public endpoints must not return 401 when accessed without session
		publicEndpoints := []struct {
			method string
			path   string
			body   string
		}{
			{"POST", "/api/register", `{"username":"pub_user","email":"pub@example.com","password":"testpassword123"}`},
			{"POST", "/api/account/forgot", `{"email":"unknown@example.com"}`},
			{"POST", "/api/account/verify", `{"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}`},
			{"POST", "/api/account/reset", `{"token":"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef","password":"newpass123456"}`},
			{"POST", "/api/galleries/" + galleryID + "/guest-uploads", ``},
			{"GET", "/api/config", ``},
			{"GET", "/api/v1/health", ``},
			{"POST", "/api/login", `{"username":"bad","password":"bad"}`},
			{"POST", "/api/logout", ``},
			{"POST", "/api/checkout", `{"email":"test@example.com","photos":[]}`},
		}

		for _, ep := range publicEndpoints {
			w := env.request(ep.method, ep.path, []byte(ep.body), "application/json", nil)
			if w.Code == http.StatusUnauthorized && strings.Contains(w.Body.String(), "Please sign in to your studio") {
				t.Errorf("public endpoint %s %s incorrectly required authentication (401)", ep.method, ep.path)
			}
		}

		// Authenticated endpoints must return 401 when unauthenticated
		protectedEndpoints := []struct {
			method string
			path   string
		}{
			{"GET", "/api/settings"},
			{"GET", "/api/galleries"},
			{"GET", "/api/orders"},
			{"GET", "/api/wallet"},
			{"GET", "/api/admin/overview"},
			{"POST", "/api/account/email"},
		}

		for _, ep := range protectedEndpoints {
			w := env.request(ep.method, ep.path, nil, "application/json", nil)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("protected endpoint %s %s did not require authentication; got %d", ep.method, ep.path, w.Code)
			}
		}

		// Non-existent route must return 404/405, NOT 503 "This feature is still being converted to MongoDB."
		w := env.request("GET", "/api/unknown-nonexistent-endpoint", nil, "", photogCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Errorf("unknown endpoint returned 503 catch-all instead of 404")
		}
	})

	t.Run("Feature02_MongoDBCollectionsAndIndexesProvisioning", func(t *testing.T) {
		ctx := env.ctx
		collections, err := env.db.ListCollectionNames(ctx, bson.M{})
		if err != nil {
			t.Fatalf("ListCollectionNames failed: %v", err)
		}

		expectedCollections := []string{"accounts", "galleries", "photos", "orders"}
		colMap := make(map[string]bool)
		for _, c := range collections {
			colMap[c] = true
		}

		for _, ec := range expectedCollections {
			if !colMap[ec] {
				t.Errorf("expected collection %s was not found in database", ec)
			}
		}

		// Verify unique index on accounts.username
		cursor, err := env.db.Collection("accounts").Indexes().List(ctx)
		if err == nil {
			defer cursor.Close(ctx)
			hasUsernameIndex := false
			for cursor.Next(ctx) {
				var idx docIndex
				if cursor.Decode(&idx) == nil {
					if idx.Key["username"] != nil {
						hasUsernameIndex = true
					}
				}
			}
			if !hasUsernameIndex {
				t.Errorf("accounts collection missing expected username index")
			}
		}
	})

	t.Run("Feature03_DynamicAppConfigAndHealthEndpoints", func(t *testing.T) {
		// Test /api/config
		w := env.request("GET", "/api/config", nil, "", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("/api/config status %d: %s", w.Code, w.Body.String())
		}
		var cfgRes struct {
			Payments        string `json:"payments"`
			EmailConfigured bool   `json:"email_configured"`
			ImgBBPreviews   bool   `json:"imgbb_previews"`
			PublicURL       string `json:"public_url"`
			R2Uploads       bool   `json:"r2_uploads"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &cfgRes); err != nil {
			t.Fatalf("/api/config json decode error: %v", err)
		}
		if cfgRes.Payments != "paystack" {
			t.Errorf("expected payments to be paystack, got %s", cfgRes.Payments)
		}
		if !cfgRes.EmailConfigured && env.cfg.SMTPHost != "" {
			t.Errorf("expected email_configured to be true when SMTPHost is set, got false")
		}

		// Test /api/v1/health
		wHealth := env.request("GET", "/api/v1/health", nil, "", nil)
		if wHealth.Code != http.StatusOK {
			t.Fatalf("/api/v1/health status %d: %s", wHealth.Code, wHealth.Body.String())
		}
		var healthRes struct {
			Status string `json:"status"`
			App    string `json:"app"`
		}
		_ = json.Unmarshal(wHealth.Body.Bytes(), &healthRes)
		if healthRes.Status != "healthy" && healthRes.Status != "ok" {
			t.Errorf("expected health status healthy or ok, got %s", healthRes.Status)
		}
	})

	t.Run("Feature04_PhotographerSelfRegistration", func(t *testing.T) {
		regPayload := map[string]string{
			"username": "new_photographer",
			"email":    "new_photographer@example.com",
			"password": "SecurePassword123!",
		}
		body, _ := json.Marshal(regPayload)
		w := env.request("POST", "/api/register", body, "application/json", nil)
		if w.Code != http.StatusCreated && w.Code != http.StatusOK {
			t.Fatalf("POST /api/register status %d: %s", w.Code, w.Body.String())
		}

		// Verify account document created in MongoDB
		var acc struct {
			Username      string `bson:"username"`
			Email         string `bson:"email"`
			EmailVerified bool   `bson:"email_verified"`
			Role          string `bson:"role"`
		}
		err := env.db.Collection("accounts").FindOne(env.ctx, bson.M{"username": "new_photographer"}).Decode(&acc)
		if err != nil {
			t.Fatalf("registered account not found in MongoDB: %v", err)
		}
		if acc.Email != "new_photographer@example.com" {
			t.Errorf("expected email new_photographer@example.com, got %s", acc.Email)
		}
		if acc.EmailVerified {
			t.Errorf("expected newly registered account to have email_verified=false")
		}
		if acc.Role != "photographer" {
			t.Errorf("expected role photographer, got %s", acc.Role)
		}
	})

	t.Run("Feature05_EmailVerification", func(t *testing.T) {
		// Provision an unverified account and a verification token link in account_links
		token, _ := security.Token()
		tokenHash := security.HashToken(token)

		_, err := env.db.Collection("account_links").InsertOne(env.ctx, bson.M{
			"token_hash": tokenHash,
			"account_id": photogID,
			"email":      "verified_target@example.com",
			"kind":       "verify",
			"expires":    time.Now().Add(1 * time.Hour).Unix(),
		})
		if err != nil {
			t.Fatalf("insert verification token: %v", err)
		}

		verifyPayload := map[string]string{"token": token}
		body, _ := json.Marshal(verifyPayload)
		w := env.request("POST", "/api/account/verify", body, "application/json", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/account/verify status %d: %s", w.Code, w.Body.String())
		}

		// Verify account is marked verified in MongoDB
		var acc struct {
			EmailVerified bool `bson:"email_verified"`
		}
		_ = env.db.Collection("accounts").FindOne(env.ctx, bson.M{"_id": photogID}).Decode(&acc)
		if !acc.EmailVerified {
			t.Errorf("account email_verified was not set to true after verification")
		}
	})

	t.Run("Feature06_PasswordResetRequest", func(t *testing.T) {
		forgotPayload := map[string]string{"email": "photog_t1@example.com"}
		body, _ := json.Marshal(forgotPayload)
		w := env.request("POST", "/api/account/forgot", body, "application/json", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/account/forgot status %d: %s", w.Code, w.Body.String())
		}

		// Verify reset link generated in account_links
		count, err := env.db.Collection("account_links").CountDocuments(env.ctx, bson.M{"account_id": photogID, "kind": "reset"})
		if err != nil || count == 0 {
			t.Errorf("expected password reset token link in account_links, count=%d err=%v", count, err)
		}
	})

	t.Run("Feature07_PasswordResetSubmission", func(t *testing.T) {
		token, _ := security.Token()
		tokenHash := security.HashToken(token)

		_, err := env.db.Collection("account_links").InsertOne(env.ctx, bson.M{
			"token_hash": tokenHash,
			"account_id": photogID,
			"kind":       "reset",
			"expires":    time.Now().Add(1 * time.Hour).Unix(),
		})
		if err != nil {
			t.Fatalf("insert reset token: %v", err)
		}

		newPassword := "NewBrandNewPassword2026!"
		resetPayload := map[string]string{"token": token, "password": newPassword}
		body, _ := json.Marshal(resetPayload)
		w := env.request("POST", "/api/account/reset", body, "application/json", nil)
		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/account/reset status %d: %s", w.Code, w.Body.String())
		}

		// Verify photographer can login with new password
		newCookie := env.login(t, "photog_t1", newPassword)
		if newCookie == nil {
			t.Errorf("could not sign in with newly set password")
		}
	})

	t.Run("Feature08_ContactEmailUpdate", func(t *testing.T) {
		emailPayload := map[string]string{
			"email":    "updated_email@example.com",
			"password": "NewBrandNewPassword2026!",
		}
		body, _ := json.Marshal(emailPayload)
		w := env.request("POST", "/api/account/email", body, "application/json", photogCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/account/email status %d: %s", w.Code, w.Body.String())
		}

		// Verify verification link generated for new email
		count, err := env.db.Collection("account_links").CountDocuments(env.ctx, bson.M{"account_id": photogID, "email": "updated_email@example.com"})
		if err != nil || count == 0 {
			t.Errorf("expected verification link in account_links for updated email, count=%d err=%v", count, err)
		}
	})

	t.Run("Feature09_BankListRetrieval", func(t *testing.T) {
		w := env.request("GET", "/api/banks", nil, "", photogCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/banks status %d: %s", w.Code, w.Body.String())
		}
		var banks []map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &banks); err != nil {
			t.Fatalf("GET /api/banks json unmarshal: %v", err)
		}
	})

	t.Run("Feature10_BankAccountResolution", func(t *testing.T) {
		w := env.request("GET", "/api/banks/resolve?bank=058&account=0123456789", nil, "", photogCookie)
		// Should either return 200 with resolved name or 400/502 with structured error, NOT 503 "This feature is still being converted to MongoDB."
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("GET /api/banks/resolve returned 503 stub: %s", w.Body.String())
		}
	})

	t.Run("Feature11_WithdrawalRequestCreation", func(t *testing.T) {
		// Insert paid order for photographer to have available funds
		_, _ = env.db.Collection("orders").InsertOne(env.ctx, bson.M{
			"id":               "ord_withdrawable_t1",
			"studio_id":        photogID,
			"amount":           int64(100000), // 1000 NGN
			"platform_fee":     int64(5000),
			"status":           "Paid",
			"payment_provider": "paystack",
			"created":          time.Now().UTC().Format(time.RFC3339Nano),
		})

		withdrawPayload := map[string]any{
			"amount":  50000,
			"account": "0123456789",
			"bank":    "058",
			"name":    "Test Studio",
		}
		body, _ := json.Marshal(withdrawPayload)
		w := env.request("POST", "/api/withdrawals", body, "application/json", photogCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/withdrawals returned 503 stub: %s", w.Body.String())
		}
	})

	t.Run("Feature12_WithdrawalStatusSync", func(t *testing.T) {
		ref := "pictide_sync_ref_1"
		_, _ = env.db.Collection("withdrawals").InsertOne(env.ctx, bson.M{
			"id":            "w_sync_1",
			"studio_id":     photogID,
			"reference":     ref,
			"amount":        int64(50000),
			"status":        "pending",
			"account_last4": "6789",
			"created":       time.Now().UTC().Format(time.RFC3339Nano),
		})

		w := env.request("POST", "/api/withdrawals/"+ref+"/sync", nil, "application/json", photogCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/withdrawals/{ref}/sync returned 503 stub: %s", w.Body.String())
		}
	})

	t.Run("Feature13_PayoutWebhookProcessing", func(t *testing.T) {
		ref := "pictide_webhook_ref_1"
		_, _ = env.db.Collection("withdrawals").InsertOne(env.ctx, bson.M{
			"id":            "w_hook_1",
			"studio_id":     photogID,
			"reference":     ref,
			"amount":        int64(50000),
			"status":        "pending",
			"account_last4": "6789",
			"created":       time.Now().UTC().Format(time.RFC3339Nano),
		})

		hookPayload := map[string]any{
			"event": "transfer.success",
			"data": map[string]any{
				"reference": ref,
				"status":    "success",
				"amount":    50000,
				"currency":  "NGN",
			},
		}
		body, _ := json.Marshal(hookPayload)
		sig := computePaystackSignature(env.cfg.PaystackSecret, body)

		w := env.requestWithHeader("POST", "/api/v1/webhooks/paystack", body, map[string]string{
			"Content-Type":         "application/json",
			"X-Paystack-Signature": sig,
		}, nil)

		if w.Code == http.StatusServiceUnavailable {
			t.Fatalf("payout webhook returned 503; transfer webhooks must be processed: %s", w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("payout webhook returned unexpected status %d: %s", w.Code, w.Body.String())
		}

		// Verify withdrawal status updated in MongoDB
		var wDoc struct {
			Status string `bson:"status"`
		}
		_ = env.db.Collection("withdrawals").FindOne(env.ctx, bson.M{"reference": ref}).Decode(&wDoc)
		if wDoc.Status != "success" {
			t.Errorf("expected withdrawal status to be updated to success, got %s", wDoc.Status)
		}
	})

	t.Run("Feature14_WalletBalanceAndConfigReporting", func(t *testing.T) {
		w := env.request("GET", "/api/wallet", nil, "", photogCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/wallet status %d: %s", w.Code, w.Body.String())
		}
		var walletRes struct {
			Earned      int64  `json:"earned"`
			Available   int64  `json:"available"`
			Configured  bool   `json:"configured"`
			Withdrawals []any  `json:"withdrawals"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &walletRes); err != nil {
			t.Fatalf("wallet json unmarshal: %v", err)
		}
		if !walletRes.Configured {
			t.Errorf("wallet.configured should be true when PaystackSecret is set, got false")
		}
	})

	t.Run("Feature15_EnableGuestUploadsToggle", func(t *testing.T) {
		customBody := []byte(`{"allow_guest_upload":1}`)
		w := env.request("PATCH", "/api/galleries/"+galleryID+"/customization", customBody, "application/json", photogCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "Guest uploads are not available yet.") {
			t.Fatalf("PATCH customization returned 503 'Guest uploads are not available yet.'")
		}
		if w.Code != http.StatusOK {
			t.Fatalf("PATCH customization status %d: %s", w.Code, w.Body.String())
		}

		// Verify persisted in MongoDB
		var g struct {
			AllowGuestUpload int `bson:"allow_guest_upload"`
		}
		_ = env.db.Collection("galleries").FindOne(env.ctx, bson.M{"id": galleryID}).Decode(&g)
		if g.AllowGuestUpload != 1 {
			t.Errorf("expected allow_guest_upload=1 in gallery document, got %d", g.AllowGuestUpload)
		}
	})

	t.Run("Feature16_GuestUploadSubmission", func(t *testing.T) {
		// Publish gallery first
		_, _ = env.db.Collection("galleries").UpdateOne(env.ctx, bson.M{"id": galleryID}, bson.M{"$set": bson.M{"status": "Published"}})

		pngData := createTestPNGBytes(100, 100, color.RGBA{R: 200, G: 100, B: 50, A: 255})
		files := map[string][]testFile{
			"photos": {{"guest_test.png", pngData}},
		}

		w := env.multipartRequest("POST", "/api/galleries/"+galleryID+"/guest-uploads", files, nil, nil)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST guest-uploads returned 503 stub: %s", w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("POST guest-uploads status %d: %s", w.Code, w.Body.String())
		}

		var res struct {
			Token string `json:"token"`
			Added []struct {
				ID     string `json:"id"`
				Name   string `json:"name"`
				Status string `json:"status"`
			} `json:"added"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res.Token == "" || len(res.Added) == 0 {
			t.Fatalf("guest-uploads did not return token or added photo: %s", w.Body.String())
		}

		// Verify record in guest_uploads collection
		var gu struct {
			ID     string `bson:"id"`
			Status string `bson:"status"`
		}
		err := env.db.Collection("guest_uploads").FindOne(env.ctx, bson.M{"id": res.Added[0].ID}).Decode(&gu)
		if err != nil {
			t.Fatalf("guest upload not found in guest_uploads collection: %v", err)
		}
		if gu.Status != "pending" {
			t.Errorf("expected status pending, got %s", gu.Status)
		}
	})

	t.Run("Feature17_GuestUploadsListing", func(t *testing.T) {
		w := env.request("GET", "/api/galleries/"+galleryID+"/guest-uploads", nil, "", photogCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("GET guest-uploads returned 503 stub: %s", w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("GET guest-uploads status %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Feature18_GuestUploadPreview", func(t *testing.T) {
		// Find a pending upload in DB
		var gu struct {
			ID string `bson:"id"`
		}
		err := env.db.Collection("guest_uploads").FindOne(env.ctx, bson.M{"gallery_id": galleryID, "status": "pending"}).Decode(&gu)
		if err == nil && gu.ID != "" {
			w := env.request("GET", "/api/guest-uploads/"+gu.ID+"/preview", nil, "", photogCookie)
			if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
				t.Fatalf("GET guest-upload preview returned 503 stub: %s", w.Body.String())
			}
		}
	})

	t.Run("Feature19_GuestUploadReview", func(t *testing.T) {
		var gu struct {
			ID string `bson:"id"`
		}
		err := env.db.Collection("guest_uploads").FindOne(env.ctx, bson.M{"gallery_id": galleryID, "status": "pending"}).Decode(&gu)
		if err == nil && gu.ID != "" {
			reviewBody := []byte(`{"action":"approve"}`)
			w := env.request("POST", "/api/guest-uploads/"+gu.ID+"/review", reviewBody, "application/json", photogCookie)
			if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
				t.Fatalf("POST guest-upload review returned 503 stub: %s", w.Body.String())
			}
		}
	})

	t.Run("Feature20_GuestUploadsStatusManagement", func(t *testing.T) {
		statusBody := []byte(`{"tokens":["sample_token_value"]}`)
		w := env.request("POST", "/api/guest-uploads/status", statusBody, "application/json", nil)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/guest-uploads/status returned 503 stub: %s", w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/guest-uploads/status status %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Feature21_StudioBrandMarkUploadAndDelete", func(t *testing.T) {
		pngData := createTestPNGBytes(50, 50, color.RGBA{R: 10, G: 200, B: 10, A: 255})
		files := map[string][]testFile{
			"logo": {{"studio_logo.png", pngData}},
		}

		w := env.multipartRequest("POST", "/api/settings/logo", files, nil, photogCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/settings/logo returned 503 stub: %s", w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/settings/logo status %d: %s", w.Code, w.Body.String())
		}

		// DELETE logo
		wDel := env.request("DELETE", "/api/settings/logo", nil, "", photogCookie)
		if wDel.Code != http.StatusOK {
			t.Fatalf("DELETE /api/settings/logo status %d: %s", wDel.Code, wDel.Body.String())
		}
	})

	t.Run("Feature22_GalleryWatermarkLogoUploadAndDelete", func(t *testing.T) {
		pngData := createTestPNGBytes(50, 50, color.RGBA{R: 20, G: 30, B: 200, A: 255})
		files := map[string][]testFile{
			"logo": {{"gallery_logo.png", pngData}},
		}

		w := env.multipartRequest("POST", "/api/galleries/"+galleryID+"/logo", files, nil, photogCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/galleries/{id}/logo returned 503 stub: %s", w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/galleries/{id}/logo status %d: %s", w.Code, w.Body.String())
		}

		wDel := env.request("DELETE", "/api/galleries/"+galleryID+"/logo", nil, "", photogCookie)
		if wDel.Code != http.StatusOK {
			t.Fatalf("DELETE /api/galleries/{id}/logo status %d: %s", wDel.Code, wDel.Body.String())
		}
	})

	t.Run("Feature23_ApplyBrandMarkAcrossPhotos", func(t *testing.T) {
		w := env.request("POST", "/api/settings/apply-brand", nil, "", photogCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/settings/apply-brand returned 503 stub: %s", w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/settings/apply-brand status %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Feature24_RegenerateGalleryPreviews", func(t *testing.T) {
		w := env.request("POST", "/api/galleries/"+galleryID+"/regenerate", nil, "", photogCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/galleries/{id}/regenerate returned 503 stub: %s", w.Body.String())
		}
		if w.Code != http.StatusOK {
			t.Fatalf("POST /api/galleries/{id}/regenerate status %d: %s", w.Code, w.Body.String())
		}
	})

	t.Run("Feature25_StudioLogoStatusInWorkspace", func(t *testing.T) {
		w := env.request("GET", "/api/settings", nil, "", photogCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/settings status %d: %s", w.Code, w.Body.String())
		}
		var sRes struct {
			StudioLogo bool `json:"studio_logo"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &sRes)
	})

	t.Run("Feature26_AdminSynchronization", func(t *testing.T) {
		syncBody := []byte(`{"accountId":"` + photogID + `","reference":"sample_order_id","kind":"order"}`)
		w := env.request("POST", "/api/admin/sync", syncBody, "application/json", adminCookie)
		if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
			t.Fatalf("POST /api/admin/sync returned 503 stub: %s", w.Body.String())
		}
	})

	t.Run("Feature27_AdminOverviewDetails", func(t *testing.T) {
		w := env.request("GET", "/api/admin/overview", nil, "", adminCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/admin/overview status %d: %s", w.Code, w.Body.String())
		}
		var overview struct {
			Accounts []struct {
				ID       string `json:"id"`
				Username string `json:"username"`
				Email    string `json:"email"`
				Verified bool   `json:"verified"`
			} `json:"accounts"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &overview); err != nil {
			t.Fatalf("admin overview json unmarshal: %v", err)
		}
	})

	t.Run("Feature28_OrdersQueryFiltering", func(t *testing.T) {
		// Populate test orders
		now := time.Now().UTC()
		_, _ = env.db.Collection("orders").InsertMany(env.ctx, []any{
			bson.M{"id": "ord_f28_1", "studio_id": photogID, "gallery_id": galleryID, "status": "Paid", "amount": int64(10000), "created": now.Add(-2 * time.Hour).Format(time.RFC3339Nano)},
			bson.M{"id": "ord_f28_2", "studio_id": photogID, "gallery_id": galleryID, "status": "Pending", "amount": int64(5000), "created": now.Add(-1 * time.Hour).Format(time.RFC3339Nano)},
			bson.M{"id": "ord_f28_3", "studio_id": photogID, "gallery_id": "other_gal", "status": "Paid", "amount": int64(20000), "created": now.Format(time.RFC3339Nano)},
		})

		// Filter by status=Paid
		w := env.request("GET", "/api/orders?status=Paid", nil, "", photogCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/orders status %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Items []struct {
				ID     string `json:"id"`
				Status string `json:"status"`
			} `json:"items"`
			Total int `json:"total"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		for _, it := range res.Items {
			if it.Status != "Paid" {
				t.Errorf("expected only Paid orders when status=Paid, got %s", it.Status)
			}
		}

		// Filter by gallery
		wGal := env.request("GET", "/api/orders?gallery="+galleryID, nil, "", photogCookie)
		if wGal.Code != http.StatusOK {
			t.Fatalf("GET /api/orders?gallery= status %d: %s", wGal.Code, wGal.Body.String())
		}
	})

	t.Run("Feature29_OrdersInsightsAndTrendsAnalytics", func(t *testing.T) {
		w := env.request("GET", "/api/orders", nil, "", photogCookie)
		if w.Code != http.StatusOK {
			t.Fatalf("GET /api/orders status %d: %s", w.Code, w.Body.String())
		}
		var res struct {
			Insights struct {
				Gross     int64 `json:"gross"`
				Paid      int   `json:"paid"`
				Pending   int   `json:"pending"`
				Days      []any `json:"days"`
				Galleries []any `json:"galleries"`
			} `json:"insights"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
			t.Fatalf("GET /api/orders insights unmarshal: %v", err)
		}
	})

	t.Run("Feature30_Zero503CatchAllAndStubResponses", func(t *testing.T) {
		// Exercise a variety of paths to verify zero 503 stub responses remain in the gateway
		testPaths := []struct {
			method string
			path   string
			cookie *http.Cookie
		}{
			{"GET", "/api/config", nil},
			{"GET", "/api/v1/health", nil},
			{"GET", "/api/session", nil},
			{"GET", "/api/session", photogCookie},
			{"GET", "/api/settings", photogCookie},
			{"GET", "/api/galleries", photogCookie},
			{"GET", "/api/orders", photogCookie},
			{"GET", "/api/wallet", photogCookie},
			{"GET", "/api/admin/overview", adminCookie},
			{"GET", "/api/admin/insights", adminCookie},
		}

		for _, p := range testPaths {
			w := env.request(p.method, p.path, nil, "", p.cookie)
			if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
				t.Errorf("endpoint %s %s returned 503 stub response: %s", p.method, p.path, w.Body.String())
			}
		}
	})
}

type docIndex struct {
	Key bson.M `bson:"key"`
}
