package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"pictide.local/studio/internal/security"
)

// TestE2E_Tier2 covers boundary conditions, corner cases, security gates, and adversarial validation.
func TestE2E_Tier2(t *testing.T) {
	env := newE2EEnv(t, "t2")

	_ = env.adminLogin(t)
	photogID, photogCookie := env.createPhotographer(t, "photog_t2", "photog_t2@example.com", "securepass123456", true)
	galleryID := env.createGallery(t, photogCookie, "Tier2 Gallery", 4000)

	t.Run("Boundary_InvalidRegistrations", func(t *testing.T) {
		tests := []struct {
			name     string
			username string
			email    string
			password string
			wantCode int
		}{
			{"short password", "validuser1", "user1@example.com", "short123", http.StatusBadRequest},
			{"invalid email", "validuser2", "notanemail", "ValidPass123456!", http.StatusBadRequest},
			{"invalid username chars", "bad user name!", "user3@example.com", "ValidPass123456!", http.StatusBadRequest},
			{"username too short", "ab", "user4@example.com", "ValidPass123456!", http.StatusBadRequest},
			{"duplicate username", "photog_t2", "new_email@example.com", "ValidPass123456!", http.StatusConflict},
			{"duplicate email", "uniqueuser123", "photog_t2@example.com", "ValidPass123456!", http.StatusConflict},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				body, _ := json.Marshal(map[string]string{
					"username": tc.username,
					"email":    tc.email,
					"password": tc.password,
				})
				w := env.request("POST", "/api/register", body, "application/json", nil)
				if w.Code == http.StatusServiceUnavailable && strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
					t.Fatalf("POST /api/register returned 503 stub: %s", w.Body.String())
				}
				if w.Code != tc.wantCode && w.Code != http.StatusBadRequest {
					t.Errorf("expected status %d or 400, got %d (body: %s)", tc.wantCode, w.Code, w.Body.String())
				}
			})
		}
	})

	t.Run("Boundary_TokenExpirationsAndReplay", func(t *testing.T) {
		// Expired verification token
		expToken, _ := security.Token()
		expHash := security.HashToken(expToken)
		_, _ = env.db.Collection("account_links").InsertOne(env.ctx, bson.M{
			"token_hash": expHash,
			"account_id": photogID,
			"kind":       "verify",
			"expires":    time.Now().Add(-10 * time.Minute).Unix(),
		})

		body, _ := json.Marshal(map[string]string{"token": expToken})
		w := env.request("POST", "/api/account/verify", body, "application/json", nil)
		if w.Code == http.StatusOK {
			t.Errorf("expired token should not be accepted, got 200 OK")
		}

		// Replay attack: once consumed, same token must fail
		validToken, _ := security.Token()
		validHash := security.HashToken(validToken)
		_, _ = env.db.Collection("account_links").InsertOne(env.ctx, bson.M{
			"token_hash": validHash,
			"account_id": photogID,
			"kind":       "verify",
			"expires":    time.Now().Add(1 * time.Hour).Unix(),
		})

		body, _ = json.Marshal(map[string]string{"token": validToken})
		wFirst := env.request("POST", "/api/account/verify", body, "application/json", nil)
		if wFirst.Code == http.StatusOK {
			// Second attempt (replay) must fail
			wSecond := env.request("POST", "/api/account/verify", body, "application/json", nil)
			if wSecond.Code == http.StatusOK {
				t.Errorf("consumed token was replayed successfully; anti-replay failed")
			}
		}

		// Malformed or non-existent token
		bodyBad, _ := json.Marshal(map[string]string{"token": "notarealtoken"})
		wBad := env.request("POST", "/api/account/verify", bodyBad, "application/json", nil)
		if wBad.Code == http.StatusOK {
			t.Errorf("malformed token should return error, got 200 OK")
		}
	})

	t.Run("Boundary_AuthAndSessionTampering", func(t *testing.T) {
		// Forged signature
		parts := strings.Split(photogCookie.Value, ".")
		if len(parts) == 4 {
			tamperedValue := fmt.Sprintf("%s.%s.%s.%s", parts[0], parts[1], parts[2], "tampered_fake_signature_12345")
			tamperedCookie := &http.Cookie{Name: "lumen_platform", Value: tamperedValue}
			w := env.request("GET", "/api/settings", nil, "", tamperedCookie)
			if w.Code != http.StatusUnauthorized {
				t.Errorf("forged session signature was accepted; got status %d", w.Code)
			}
		}

		// Expired session timestamp
		expiredTimestamp := time.Now().Add(-1 * time.Hour).UnixMilli()
		expCookieVal := fmt.Sprintf("%s.%d.ver.fake", photogID, expiredTimestamp)
		expCookie := &http.Cookie{Name: "lumen_platform", Value: expCookieVal}
		wExp := env.request("GET", "/api/settings", nil, "", expCookie)
		if wExp.Code != http.StatusUnauthorized {
			t.Errorf("expired session cookie was accepted; got status %d", wExp.Code)
		}

		// Disabled / Suspended account
		suspendedID, suspendedCookie := env.createPhotographer(t, "suspended_user", "suspended@example.com", "Password123456!", true)
		_, _ = env.db.Collection("accounts").UpdateOne(env.ctx, bson.M{"_id": suspendedID}, bson.M{"$set": bson.M{"disabled": true}})

		wSuspended := env.request("GET", "/api/settings", nil, "", suspendedCookie)
		if wSuspended.Code != http.StatusUnauthorized {
			t.Errorf("suspended account accessed protected endpoint; got status %d", wSuspended.Code)
		}
	})

	t.Run("Boundary_FinancialLimitsAndNUBANValidation", func(t *testing.T) {
		// 1. Negative amount
		negBody, _ := json.Marshal(map[string]any{"amount": -5000, "account": "0123456789", "bank": "058", "name": "Test"})
		wNeg := env.request("POST", "/api/withdrawals", negBody, "application/json", photogCookie)
		if wNeg.Code == http.StatusOK {
			t.Errorf("negative withdrawal amount was accepted; got 200 OK")
		}

		// 2. Zero amount
		zeroBody, _ := json.Marshal(map[string]any{"amount": 0, "account": "0123456789", "bank": "058", "name": "Test"})
		wZero := env.request("POST", "/api/withdrawals", zeroBody, "application/json", photogCookie)
		if wZero.Code == http.StatusOK {
			t.Errorf("zero withdrawal amount was accepted; got 200 OK")
		}

		// 3. Amount exceeding balance (balance is 0 for newly created photographer)
		overBody, _ := json.Marshal(map[string]any{"amount": 999999999, "account": "0123456789", "bank": "058", "name": "Test"})
		wOver := env.request("POST", "/api/withdrawals", overBody, "application/json", photogCookie)
		if wOver.Code == http.StatusOK {
			t.Errorf("withdrawal exceeding balance was accepted; got 200 OK")
		}

		// 4. Invalid account number (not 10 digits)
		badNUBAN, _ := json.Marshal(map[string]any{"amount": 1000, "account": "12345", "bank": "058", "name": "Test"})
		wBadNUBAN := env.request("POST", "/api/withdrawals", badNUBAN, "application/json", photogCookie)
		if wBadNUBAN.Code == http.StatusOK {
			t.Errorf("invalid 5-digit account number was accepted; got 200 OK")
		}
	})

	t.Run("Boundary_GuestUploadRestrictions", func(t *testing.T) {
		pngData := createTestPNGBytes(60, 60, nil)

		// 1. Upload to non-existent gallery
		files := map[string][]testFile{"photos": {{"test.png", pngData}}}
		wNon := env.multipartRequest("POST", "/api/galleries/non_existent_gallery_id/guest-uploads", files, nil, nil)
		if wNon.Code != http.StatusNotFound && wNon.Code != http.StatusForbidden && wNon.Code != http.StatusBadRequest {
			t.Errorf("upload to non-existent gallery got unexpected status: %d", wNon.Code)
		}

		// 2. Upload to unpublished gallery
		draftGalID := env.createGallery(t, photogCookie, "Draft Gallery", 2000)
		_, _ = env.db.Collection("galleries").UpdateOne(env.ctx, bson.M{"id": draftGalID}, bson.M{"$set": bson.M{"status": "Draft", "allow_guest_upload": 1}})
		wDraft := env.multipartRequest("POST", "/api/galleries/"+draftGalID+"/guest-uploads", files, nil, nil)
		if wDraft.Code != http.StatusForbidden && wDraft.Code != http.StatusBadRequest {
			t.Errorf("upload to draft gallery should be rejected (403/400); got %d", wDraft.Code)
		}

		// 3. Upload when allow_guest_upload=0
		_, _ = env.db.Collection("galleries").UpdateOne(env.ctx, bson.M{"id": draftGalID}, bson.M{"$set": bson.M{"status": "Published", "allow_guest_upload": 0}})
		wDisabled := env.multipartRequest("POST", "/api/galleries/"+draftGalID+"/guest-uploads", files, nil, nil)
		if wDisabled.Code != http.StatusForbidden && wDisabled.Code != http.StatusBadRequest {
			t.Errorf("upload when guest uploads disabled should be rejected (403/400); got %d", wDisabled.Code)
		}

		// 4. Upload non-image data
		fakeFiles := map[string][]testFile{"photos": {{"malicious.exe", []byte("MZbinaryexecutabledata")}} }
		_, _ = env.db.Collection("galleries").UpdateOne(env.ctx, bson.M{"id": draftGalID}, bson.M{"$set": bson.M{"allow_guest_upload": 1}})
		wBadFile := env.multipartRequest("POST", "/api/galleries/"+draftGalID+"/guest-uploads", fakeFiles, nil, nil)
		if wBadFile.Code == http.StatusOK {
			t.Errorf("non-image executable file was accepted as guest upload; got 200 OK")
		}

		// 5. Exceeding batch limit (>5 files)
		tooManyFiles := map[string][]testFile{
			"photos": {
				{"f1.png", pngData}, {"f2.png", pngData}, {"f3.png", pngData},
				{"f4.png", pngData}, {"f5.png", pngData}, {"f6.png", pngData},
			},
		}
		wTooMany := env.multipartRequest("POST", "/api/galleries/"+draftGalID+"/guest-uploads", tooManyFiles, nil, nil)
		if wTooMany.Code == http.StatusOK {
			t.Errorf("batch of 6 files should be rejected (max 5); got 200 OK")
		}

		// 6. Review with invalid action
		badActionBody, _ := json.Marshal(map[string]string{"action": "drop_database"})
		wBadAction := env.request("POST", "/api/guest-uploads/some_upload_id/review", badActionBody, "application/json", photogCookie)
		if wBadAction.Code == http.StatusOK {
			t.Errorf("invalid review action was accepted; got 200 OK")
		}
	})

	t.Run("Boundary_BrandingOversizedAndInvalid", func(t *testing.T) {
		// Corrupted image data
		corruptedFiles := map[string][]testFile{"logo": {{"bad.png", []byte("not_really_a_png")}} }
		wBadLogo := env.multipartRequest("POST", "/api/settings/logo", corruptedFiles, nil, photogCookie)
		if wBadLogo.Code == http.StatusOK {
			t.Errorf("corrupt logo image data was accepted; got 200 OK")
		}

		// Customization with invalid watermark opacity (>100)
		badCustom, _ := json.Marshal(map[string]any{"watermark_opacity": 150})
		wBadCustom := env.request("PATCH", "/api/galleries/"+galleryID+"/customization", badCustom, "application/json", photogCookie)
		if wBadCustom.Code == http.StatusOK {
			t.Errorf("watermark_opacity > 100 was accepted; got 200 OK")
		}

		// Invalid watermark style
		badStyle, _ := json.Marshal(map[string]any{"watermark_style": "upside_down_circle"})
		wBadStyle := env.request("PATCH", "/api/galleries/"+galleryID+"/customization", badStyle, "application/json", photogCookie)
		if wBadStyle.Code == http.StatusOK {
			t.Errorf("invalid watermark style was accepted; got 200 OK")
		}
	})

	t.Run("Boundary_OrdersFilterEdgeCases", func(t *testing.T) {
		// Filter with non-existent gallery
		wNonGal := env.request("GET", "/api/orders?gallery=definitely_not_a_real_gallery_id", nil, "", photogCookie)
		if wNonGal.Code != http.StatusOK {
			t.Fatalf("GET /api/orders status %d: %s", wNonGal.Code, wNonGal.Body.String())
		}
		var res struct {
			Items []any `json:"items"`
			Total int   `json:"total"`
		}
		_ = json.Unmarshal(wNonGal.Body.Bytes(), &res)
		if len(res.Items) != 0 || res.Total != 0 {
			t.Errorf("expected 0 items for non-existent gallery filter, got %d", len(res.Items))
		}

		// Inverted date range (from > to)
		wInverted := env.request("GET", "/api/orders?from=2026-12-31T00:00:00Z&to=2026-01-01T00:00:00Z", nil, "", photogCookie)
		if wInverted.Code != http.StatusOK {
			t.Fatalf("GET /api/orders inverted date status %d: %s", wInverted.Code, wInverted.Body.String())
		}
	})

	t.Run("Boundary_RolePermissionsElevation", func(t *testing.T) {
		// Non-admin (photographer) attempting to access admin endpoints must be rejected with 403 Forbidden
		adminEndpoints := []struct {
			method string
			path   string
			body   string
		}{
			{"GET", "/api/admin/overview", ""},
			{"GET", "/api/admin/insights", ""},
			{"POST", "/api/admin/accounts", `{"username":"elevated","password":"Password123456!"}`},
			{"GET", "/api/admin/accounts", ""},
			{"GET", "/api/admin/activity?account=legacy", ""},
			{"POST", "/api/admin/account-status", `{"id":"some_id","disabled":true}`},
			{"POST", "/api/admin/sync", `{"accountId":"legacy","reference":"ref","kind":"order"}`},
		}

		for _, ep := range adminEndpoints {
			var bodyBytes []byte
			if ep.body != "" {
				bodyBytes = []byte(ep.body)
			}
			w := env.request(ep.method, ep.path, bodyBytes, "application/json", photogCookie)
			if w.Code != http.StatusForbidden {
				t.Errorf("photographer was able to access admin endpoint %s %s; expected 403, got %d", ep.method, ep.path, w.Code)
			}
		}
	})

	t.Run("Boundary_MalformedJSONAndOversizedPayloads", func(t *testing.T) {
		// Broken JSON syntax
		brokenJSON := []byte(`{"username": "broken", invalid_json}`)
		wBroken := env.request("POST", "/api/register", brokenJSON, "application/json", nil)
		if wBroken.Code == http.StatusOK {
			t.Errorf("malformed JSON body was accepted; got 200 OK")
		}

		// Oversized body (>2MB MaxBytesReader limit)
		hugeBody := bytes.Repeat([]byte("A"), (2<<20)+1024)
		wHuge := env.request("POST", "/api/register", hugeBody, "application/json", nil)
		if wHuge.Code == http.StatusOK {
			t.Errorf("oversized body >2MB was accepted; got 200 OK")
		}
	})
}
