package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/mongostore"
)

// getIndexSpecs returns list of index documents as bson.M for a collection.
func getIndexSpecs(ctx context.Context, coll *mongo.Collection) ([]bson.M, error) {
	cur, err := coll.Indexes().List(ctx)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var specs []bson.M
	if err := cur.All(ctx, &specs); err != nil {
		return nil, err
	}
	return specs, nil
}

func getKeyMap(raw any) map[string]any {
	out := make(map[string]any)
	switch v := raw.(type) {
	case bson.D:
		for _, elem := range v {
			out[elem.Key] = elem.Value
		}
	case bson.M:
		for k, val := range v {
			out[k] = val
		}
	}
	return out
}

// TestChallengerM1_IndexSpecifications_EmpiricalInspection queries MongoDB directly
// to inspect the actual created index models, keys, uniqueness, and TTL settings.
func TestChallengerM1_IndexSpecifications_EmpiricalInspection(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := config.Config{
		SigningKey:    strings.Repeat("c", 32),
		AdminPassword: "adversarial-challenger-pwd-123",
	}

	_, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("httpapi.New failed: %v", err)
	}

	// 1. Check account_links indexes: token unique, expires_at TTL
	linksIndexes, err := getIndexSpecs(ctx, db.Collection("account_links"))
	if err != nil {
		t.Fatalf("failed to list account_links indexes: %v", err)
	}

	foundTokenUnique := false
	foundExpiresAtTTL := false
	var ttlSeconds int64 = -1

	for _, spec := range linksIndexes {
		keyDoc := getKeyMap(spec["key"])
		if _, ok := keyDoc["token"]; ok && spec["unique"] == true {
			foundTokenUnique = true
		}
		if _, ok := keyDoc["expires_at"]; ok {
			foundExpiresAtTTL = true
			switch v := spec["expireAfterSeconds"].(type) {
			case int32:
				ttlSeconds = int64(v)
			case int64:
				ttlSeconds = v
			case float64:
				ttlSeconds = int64(v)
			}
		}
	}

	if !foundTokenUnique {
		t.Errorf("account_links: missing unique index on 'token'")
	}
	if !foundExpiresAtTTL {
		t.Errorf("account_links: missing TTL index on 'expires_at'")
	} else if ttlSeconds != 0 {
		t.Errorf("account_links: expected expireAfterSeconds == 0, got %d", ttlSeconds)
	}

	// 2. Check accounts indexes: username unique
	accountsIndexes, err := getIndexSpecs(ctx, db.Collection("accounts"))
	if err != nil {
		t.Fatalf("failed to list accounts indexes: %v", err)
	}
	foundUsernameUnique := false
	for _, spec := range accountsIndexes {
		keyDoc := getKeyMap(spec["key"])
		if keyDoc["username"] != nil && spec["unique"] == true {
			foundUsernameUnique = true
		}
	}
	if !foundUsernameUnique {
		t.Errorf("accounts: missing unique index on 'username'")
	}

	// 3. Check withdrawals indexes: id unique, studio_id+created_at compound
	wdIndexes, err := getIndexSpecs(ctx, db.Collection("withdrawals"))
	if err != nil {
		t.Fatalf("failed to list withdrawals indexes: %v", err)
	}
	foundWdIDUnique := false
	foundWdCompound := false
	for _, spec := range wdIndexes {
		keyDoc := getKeyMap(spec["key"])
		if len(keyDoc) == 1 && keyDoc["id"] != nil && spec["unique"] == true {
			foundWdIDUnique = true
		}
		if len(keyDoc) == 2 && keyDoc["studio_id"] != nil && keyDoc["created_at"] != nil {
			foundWdCompound = true
		}
	}
	if !foundWdIDUnique {
		t.Errorf("withdrawals: missing unique index on 'id'")
	}
	if !foundWdCompound {
		t.Errorf("withdrawals: missing compound index on studio_id + created_at")
	}

	// 4. Check guest_uploads indexes: id unique, gallery_id+created_at compound
	guIndexes, err := getIndexSpecs(ctx, db.Collection("guest_uploads"))
	if err != nil {
		t.Fatalf("failed to list guest_uploads indexes: %v", err)
	}
	foundGuIDUnique := false
	foundGuCompound := false
	for _, spec := range guIndexes {
		keyDoc := getKeyMap(spec["key"])
		if len(keyDoc) == 1 && keyDoc["id"] != nil && spec["unique"] == true {
			foundGuIDUnique = true
		}
		if len(keyDoc) == 2 && keyDoc["gallery_id"] != nil && keyDoc["created_at"] != nil {
			foundGuCompound = true
		}
	}
	if !foundGuIDUnique {
		t.Errorf("guest_uploads: missing unique index on 'id'")
	}
	if !foundGuCompound {
		t.Errorf("guest_uploads: missing compound index on gallery_id + created_at")
	}

	// 5. Check admin_audit indexes: id unique
	auditIndexes, err := getIndexSpecs(ctx, db.Collection("admin_audit"))
	if err != nil {
		t.Fatalf("failed to list admin_audit indexes: %v", err)
	}
	foundAuditIDUnique := false
	for _, spec := range auditIndexes {
		keyDoc := getKeyMap(spec["key"])
		if len(keyDoc) == 1 && keyDoc["id"] != nil && spec["unique"] == true {
			foundAuditIDUnique = true
		}
	}
	if !foundAuditIDUnique {
		t.Errorf("admin_audit: missing unique index on 'id'")
	}
}

// TestChallengerM1_IdempotentIndexCreation tests that index initialization is completely
// idempotent and does not fail when called repeatedly against existing schemas.
func TestChallengerM1_IdempotentIndexCreation(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := config.Config{
		SigningKey:    strings.Repeat("c", 32),
		AdminPassword: "adversarial-challenger-pwd-123",
	}

	// First initialization
	_, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("first New failed: %v", err)
	}

	// Second initialization on the same DB instance
	_, err = New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("second New failed (not idempotent): %v", err)
	}

	// Individual store EnsureIndexes idempotency
	wdStore, err := mongostore.NewWithdrawals(db)
	if err != nil {
		t.Fatalf("NewWithdrawals failed: %v", err)
	}
	if err := wdStore.EnsureIndexes(ctx); err != nil {
		t.Errorf("wdStore.EnsureIndexes failed: %v", err)
	}

	guStore, err := mongostore.NewGuestUploads(db)
	if err != nil {
		t.Fatalf("NewGuestUploads failed: %v", err)
	}
	if err := guStore.EnsureIndexes(ctx); err != nil {
		t.Errorf("guStore.EnsureIndexes failed: %v", err)
	}

	alStore, err := mongostore.NewAccountLinks(db)
	if err != nil {
		t.Fatalf("NewAccountLinks failed: %v", err)
	}
	if err := alStore.EnsureIndexes(ctx); err != nil {
		t.Errorf("alStore.EnsureIndexes failed: %v", err)
	}

	auditStore, err := mongostore.NewAdminAudit(db)
	if err != nil {
		t.Fatalf("NewAdminAudit failed: %v", err)
	}
	if err := auditStore.EnsureIndexes(ctx); err != nil {
		t.Errorf("auditStore.EnsureIndexes failed: %v", err)
	}
}

// TestChallengerM1_ConcurrentDuplicateRejection launches concurrent goroutines attempting
// to insert identical records to verify race condition resilience and duplicate rejection.
func TestChallengerM1_ConcurrentDuplicateRejection(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	cfg := config.Config{
		SigningKey:    strings.Repeat("c", 32),
		AdminPassword: "adversarial-challenger-pwd-123",
	}

	_, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	const concurrency = 10

	t.Run("ConcurrentAccountUsernameDuplicates", func(t *testing.T) {
		coll := db.Collection("accounts")
		var successCount int64
		var dupCount int64
		var wg sync.WaitGroup

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				_, err := coll.InsertOne(ctx, bson.M{
					"_id":      bson.NewObjectID().Hex(),
					"username": "race_contender",
					"role":     "photographer",
				})
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else if mongo.IsDuplicateKeyError(err) {
					atomic.AddInt64(&dupCount, 1)
				} else {
					t.Errorf("unexpected error on concurrent insert: %v", err)
				}
			}(i)
		}
		wg.Wait()

		if successCount != 1 {
			t.Errorf("expected exactly 1 successful insert, got %d", successCount)
		}
		if dupCount != concurrency-1 {
			t.Errorf("expected %d duplicate key errors, got %d", concurrency-1, dupCount)
		}
	})

	t.Run("ConcurrentWithdrawalIDDuplicates", func(t *testing.T) {
		wdStore, err := mongostore.NewWithdrawals(db)
		if err != nil {
			t.Fatalf("NewWithdrawals failed: %v", err)
		}

		var successCount int64
		var dupCount int64
		var wg sync.WaitGroup

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				err := wdStore.Create(ctx, &mongostore.Withdrawal{
					ID:            "wd_race_unique_id",
					StudioID:      "studio_race",
					Amount:        10000,
					Reference:     bson.NewObjectID().Hex(),
					AccountNumber: "1234567890",
				})
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else if mongo.IsDuplicateKeyError(err) {
					atomic.AddInt64(&dupCount, 1)
				} else {
					t.Errorf("unexpected error on concurrent insert: %v", err)
				}
			}(i)
		}
		wg.Wait()

		if successCount != 1 {
			t.Errorf("expected exactly 1 successful insert, got %d", successCount)
		}
		if dupCount != concurrency-1 {
			t.Errorf("expected %d duplicate key errors, got %d", concurrency-1, dupCount)
		}
	})

	t.Run("ConcurrentGuestUploadIDDuplicates", func(t *testing.T) {
		guStore, err := mongostore.NewGuestUploads(db)
		if err != nil {
			t.Fatalf("NewGuestUploads failed: %v", err)
		}

		var successCount int64
		var dupCount int64
		var wg sync.WaitGroup

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				err := guStore.Create(ctx, &mongostore.GuestUpload{
					ID:        "gu_race_unique_id",
					StudioID:  "studio_race",
					GalleryID: "gallery_race",
					Filename:  "race.jpg",
				})
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else if mongo.IsDuplicateKeyError(err) {
					atomic.AddInt64(&dupCount, 1)
				} else {
					t.Errorf("unexpected error on concurrent insert: %v", err)
				}
			}(i)
		}
		wg.Wait()

		if successCount != 1 {
			t.Errorf("expected exactly 1 successful insert, got %d", successCount)
		}
		if dupCount != concurrency-1 {
			t.Errorf("expected %d duplicate key errors, got %d", concurrency-1, dupCount)
		}
	})

	t.Run("ConcurrentAccountLinkTokenDuplicates", func(t *testing.T) {
		alStore, err := mongostore.NewAccountLinks(db)
		if err != nil {
			t.Fatalf("NewAccountLinks failed: %v", err)
		}

		var successCount int64
		var dupCount int64
		var wg sync.WaitGroup

		for i := 0; i < concurrency; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				err := alStore.Create(ctx, &mongostore.AccountLink{
					Token:     "tok_race_unique_token",
					AccountID: "acc_race",
					Purpose:   "verify_email",
					ExpiresAt: time.Now().Add(1 * time.Hour),
				})
				if err == nil {
					atomic.AddInt64(&successCount, 1)
				} else if mongo.IsDuplicateKeyError(err) {
					atomic.AddInt64(&dupCount, 1)
				} else {
					t.Errorf("unexpected error on concurrent insert: %v", err)
				}
			}(i)
		}
		wg.Wait()

		if successCount != 1 {
			t.Errorf("expected exactly 1 successful insert, got %d", successCount)
		}
		if dupCount != concurrency-1 {
			t.Errorf("expected %d duplicate key errors, got %d", concurrency-1, dupCount)
		}
	})
}

// TestChallengerM1_TTLIndex_And_TimeQueries tests the behavioral mechanics of TTL indexing
// and verifies that query-level time filters prevent expired tokens from being retrieved.
func TestChallengerM1_TTLIndex_And_TimeQueries(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	alStore, err := mongostore.NewAccountLinks(db)
	if err != nil {
		t.Fatalf("NewAccountLinks failed: %v", err)
	}
	if err := alStore.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes failed: %v", err)
	}

	// 1. Create a future link (valid)
	futureLink := &mongostore.AccountLink{
		Token:     "tok_valid_future_123",
		AccountID: "acc_1",
		Purpose:   "verify_email",
		ExpiresAt: time.Now().Add(2 * time.Hour),
	}
	if err := alStore.Create(ctx, futureLink); err != nil {
		t.Fatalf("Create future link failed: %v", err)
	}

	// 2. Create an already expired link
	expiredLink := &mongostore.AccountLink{
		Token:     "tok_expired_past_456",
		AccountID: "acc_2",
		Purpose:   "verify_email",
		ExpiresAt: time.Now().Add(-10 * time.Minute),
	}
	if err := alStore.Create(ctx, expiredLink); err != nil {
		t.Fatalf("Create expired link failed: %v", err)
	}

	// 3. GetValid must return future link
	foundValid, err := alStore.GetValid(ctx, "tok_valid_future_123", "verify_email")
	if err != nil {
		t.Fatalf("GetValid for future link failed: %v", err)
	}
	if foundValid.Token != "tok_valid_future_123" {
		t.Errorf("unexpected token: %s", foundValid.Token)
	}

	// 4. GetValid must REJECT expired link
	_, err = alStore.GetValid(ctx, "tok_expired_past_456", "verify_email")
	if err == nil {
		t.Fatalf("expected GetValid for expired link to fail, but succeeded")
	}
	if !errors.Is(err, mongo.ErrNoDocuments) {
		t.Errorf("expected mongo.ErrNoDocuments, got: %v", err)
	}

	// 5. GetValid with mismatched purpose must fail
	_, err = alStore.GetValid(ctx, "tok_valid_future_123", "password_reset")
	if err == nil {
		t.Fatalf("expected GetValid with wrong purpose to fail, but succeeded")
	}

	// 6. DeleteByToken and DeleteByAccountAndPurpose
	if err := alStore.DeleteByToken(ctx, "tok_valid_future_123"); err != nil {
		t.Errorf("DeleteByToken failed: %v", err)
	}
	_, err = alStore.GetByToken(ctx, "tok_valid_future_123")
	if !errors.Is(err, mongo.ErrNoDocuments) {
		t.Errorf("expected ErrNoDocuments after delete, got %v", err)
	}
}

// TestChallengerM1_StoreConstructors_NilSafety stress-tests store methods against
// nil references, missing database connections, and malformed inputs.
func TestChallengerM1_StoreConstructors_NilSafety(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	// 1. Nil DB constructors must return errors
	if _, err := mongostore.NewWithdrawals(nil); err == nil {
		t.Error("expected error for nil db in NewWithdrawals")
	}
	if _, err := mongostore.NewGuestUploads(nil); err == nil {
		t.Error("expected error for nil db in NewGuestUploads")
	}
	if _, err := mongostore.NewAccountLinks(nil); err == nil {
		t.Error("expected error for nil db in NewAccountLinks")
	}
	if _, err := mongostore.NewAdminAudit(nil); err == nil {
		t.Error("expected error for nil db in NewAdminAudit")
	}

	// 2. Withdrawals store input validation
	wdStore, _ := mongostore.NewWithdrawals(db)
	if err := wdStore.Create(ctx, nil); err == nil {
		t.Error("wdStore.Create(nil) should fail")
	}
	if err := wdStore.Create(ctx, &mongostore.Withdrawal{}); err == nil {
		t.Error("wdStore.Create with empty withdrawal should fail")
	}
	if _, err := wdStore.GetByID(ctx, ""); err == nil {
		t.Error("wdStore.GetByID(\"\") should fail")
	}
	if _, err := wdStore.GetByReference(ctx, ""); err == nil {
		t.Error("wdStore.GetByReference(\"\") should fail")
	}
	if _, err := wdStore.ListByStudio(ctx, "", 10); err == nil {
		t.Error("wdStore.ListByStudio(\"\") should fail")
	}
	if err := wdStore.UpdateStatus(ctx, "", "success", 0); err == nil {
		t.Error("wdStore.UpdateStatus with empty reference should fail")
	}
	if err := wdStore.UpdateStatus(ctx, "non_existent_ref", "success", 0); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Errorf("expected ErrNoDocuments for non-existent reference, got %v", err)
	}
	if _, err := wdStore.CountPendingByStudio(ctx, ""); err == nil {
		t.Error("wdStore.CountPendingByStudio(\"\") should fail")
	}

	// 3. GuestUploads store input validation
	guStore, _ := mongostore.NewGuestUploads(db)
	if err := guStore.Create(ctx, nil); err == nil {
		t.Error("guStore.Create(nil) should fail")
	}
	if err := guStore.Create(ctx, &mongostore.GuestUpload{}); err == nil {
		t.Error("guStore.Create with empty item should fail")
	}
	if _, err := guStore.GetByID(ctx, ""); err == nil {
		t.Error("guStore.GetByID(\"\") should fail")
	}
	if _, err := guStore.ListByGallery(ctx, ""); err == nil {
		t.Error("guStore.ListByGallery(\"\") should fail")
	}
	if _, err := guStore.ListByStudio(ctx, ""); err == nil {
		t.Error("guStore.ListByStudio(\"\") should fail")
	}
	if err := guStore.UpdateStatus(ctx, "non_existent_id", "approved"); !errors.Is(err, mongo.ErrNoDocuments) {
		t.Errorf("expected ErrNoDocuments for non-existent id, got %v", err)
	}

	// 4. AdminAudit store input validation
	auditStore, _ := mongostore.NewAdminAudit(db)
	if err := auditStore.Record(ctx, nil); err == nil {
		t.Error("auditStore.Record(nil) should fail")
	}
	if err := auditStore.Record(ctx, &mongostore.AdminAuditEvent{}); err == nil {
		t.Error("auditStore.Record with empty event should fail")
	}
}

// TestChallengerM1_HealthCheck_SeveredClient tests that /api/v1/health returns 503
// when a previously live MongoDB client connection is severed.
func TestChallengerM1_HealthCheck_SeveredClient(t *testing.T) {
	client, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	cfg := config.Config{
		SigningKey:    strings.Repeat("c", 32),
		AdminPassword: "adversarial-challenger-pwd-123",
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// 1. Initial health check on connected cluster -> 200 OK
	w1 := httptest.NewRecorder()
	r1 := httptest.NewRequest("GET", "/api/v1/health", nil)
	server.ServeHTTP(w1, r1)
	if w1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w1.Code, w1.Body.String())
	}

	// 2. Sever the client connection directly
	discCtx, discCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer discCancel()
	_ = client.Disconnect(discCtx)

	// 3. Health check on severed connection -> MUST return 503 Service Unavailable
	w2 := httptest.NewRecorder()
	r2 := httptest.NewRequest("GET", "/api/v1/health", nil)
	server.ServeHTTP(w2, r2)

	if w2.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected HTTP 503 for severed connection, got %d: %s", w2.Code, w2.Body.String())
	}

	var res map[string]string
	if err := json.Unmarshal(w2.Body.Bytes(), &res); err != nil {
		t.Fatalf("failed to decode health response: %v", err)
	}
	if res["status"] != "unavailable" || res["database"] != "disconnected" {
		t.Errorf("expected unavailable/disconnected, got %+v", res)
	}

	// 4. HEAD request on /api/v1/health should return 503 when disconnected
	w3 := httptest.NewRecorder()
	r3 := httptest.NewRequest("HEAD", "/api/v1/health", nil)
	server.ServeHTTP(w3, r3)
	if w3.Code != http.StatusServiceUnavailable {
		t.Errorf("HEAD on disconnected health should return 503, got %d", w3.Code)
	}
}

// TestChallengerM1_StoreRepositoryLifecycle verifies full CRUD and querying
// semantics for withdrawals, guest uploads, and admin audit against MongoDB.
func TestChallengerM1_StoreRepositoryLifecycle(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	// 1. Withdrawals Lifecycle
	wdStore, err := mongostore.NewWithdrawals(db)
	if err != nil {
		t.Fatalf("NewWithdrawals failed: %v", err)
	}
	if err := wdStore.EnsureIndexes(ctx); err != nil {
		t.Fatalf("EnsureIndexes failed: %v", err)
	}

	wd1 := &mongostore.Withdrawal{
		ID:            "wd_item_001",
		StudioID:      "studio_alpha",
		Amount:        50000,
		Fee:           100,
		Currency:      "NGN",
		RecipientCode: "RCP_123",
		Reference:     "ref_alpha_1",
		Status:        "pending",
		BankCode:      "058",
		AccountNumber: "0123456789",
	}
	if err := wdStore.Create(ctx, wd1); err != nil {
		t.Fatalf("wdStore.Create failed: %v", err)
	}

	// AccountLast4 auto-population check
	fetchedWd, err := wdStore.GetByID(ctx, "wd_item_001")
	if err != nil {
		t.Fatalf("wdStore.GetByID failed: %v", err)
	}
	if fetchedWd.AccountLast4 != "6789" {
		t.Errorf("expected AccountLast4 '6789', got '%s'", fetchedWd.AccountLast4)
	}

	// Status update
	if err := wdStore.UpdateStatus(ctx, "ref_alpha_1", "success", time.Now().Unix()); err != nil {
		t.Fatalf("wdStore.UpdateStatus failed: %v", err)
	}
	fetchedWd2, err := wdStore.GetByReference(ctx, "ref_alpha_1")
	if err != nil {
		t.Fatalf("wdStore.GetByReference failed: %v", err)
	}
	if fetchedWd2.Status != "success" {
		t.Errorf("expected status 'success', got '%s'", fetchedWd2.Status)
	}

	// Pending count
	wdPending := &mongostore.Withdrawal{
		ID:            "wd_item_002",
		StudioID:      "studio_alpha",
		Amount:        30000,
		Reference:     "ref_alpha_2",
		Status:        "otp",
		AccountNumber: "9876543210",
	}
	_ = wdStore.Create(ctx, wdPending)
	pendingCount, err := wdStore.CountPendingByStudio(ctx, "studio_alpha")
	if err != nil {
		t.Fatalf("CountPendingByStudio failed: %v", err)
	}
	if pendingCount != 1 {
		t.Errorf("expected 1 pending withdrawal (status=otp), got %d", pendingCount)
	}

	// 2. GuestUploads Lifecycle
	guStore, err := mongostore.NewGuestUploads(db)
	if err != nil {
		t.Fatalf("NewGuestUploads failed: %v", err)
	}
	if err := guStore.EnsureIndexes(ctx); err != nil {
		t.Fatalf("guStore.EnsureIndexes failed: %v", err)
	}

	gu1 := &mongostore.GuestUpload{
		ID:        "gu_001",
		StudioID:  "studio_alpha",
		GalleryID: "gal_100",
		GuestName: "Alice",
		Filename:  "wedding_alice.jpg",
		Size:      204800,
	}
	if err := guStore.Create(ctx, gu1); err != nil {
		t.Fatalf("guStore.Create failed: %v", err)
	}

	// Default status should be "pending"
	fetchedGu, err := guStore.GetByID(ctx, "gu_001")
	if err != nil {
		t.Fatalf("guStore.GetByID failed: %v", err)
	}
	if fetchedGu.Status != "pending" {
		t.Errorf("expected default status 'pending', got '%s'", fetchedGu.Status)
	}
	if fetchedGu.Name != "wedding_alice.jpg" {
		t.Errorf("expected Name to default to Filename, got '%s'", fetchedGu.Name)
	}

	// Update status to approved
	if err := guStore.UpdateStatus(ctx, "gu_001", "approved"); err != nil {
		t.Fatalf("guStore.UpdateStatus failed: %v", err)
	}

	// ListByIDs
	list, err := guStore.ListByIDs(ctx, []string{"gu_001"})
	if err != nil {
		t.Fatalf("guStore.ListByIDs failed: %v", err)
	}
	if len(list) != 1 || list[0].Status != "approved" {
		t.Errorf("unexpected ListByIDs result: %+v", list)
	}

	// 3. AdminAudit Lifecycle & Sorting
	auditStore, err := mongostore.NewAdminAudit(db)
	if err != nil {
		t.Fatalf("NewAdminAudit failed: %v", err)
	}
	if err := auditStore.EnsureIndexes(ctx); err != nil {
		t.Fatalf("auditStore.EnsureIndexes failed: %v", err)
	}

	t1 := time.Now().Unix() - 100
	t2 := time.Now().Unix() - 50
	t3 := time.Now().Unix()

	_ = auditStore.Record(ctx, &mongostore.AdminAuditEvent{ID: "audit_1", Action: "first", CreatedAt: t1})
	_ = auditStore.Record(ctx, &mongostore.AdminAuditEvent{ID: "audit_2", Action: "second", CreatedAt: t2})
	_ = auditStore.Record(ctx, &mongostore.AdminAuditEvent{ID: "audit_3", Action: "third", CreatedAt: t3})

	recent, err := auditStore.ListRecent(ctx, 10)
	if err != nil {
		t.Fatalf("auditStore.ListRecent failed: %v", err)
	}
	if len(recent) < 3 {
		t.Fatalf("expected at least 3 audit events, got %d", len(recent))
	}
	// Verify descending order
	if recent[0].ID != "audit_3" || recent[1].ID != "audit_2" || recent[2].ID != "audit_1" {
		t.Errorf("audit events not sorted descending by created_at: %v, %v, %v", recent[0].ID, recent[1].ID, recent[2].ID)
	}
}

func getTestSessionCookie(t *testing.T, server *Server, adminPass string) *http.Cookie {
	t.Helper()
	loginBody := strings.NewReader(`{"username":"admin","password":"` + adminPass + `"}`)
	r := httptest.NewRequest("POST", "/api/login", loginBody)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	server.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("failed to login: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 {
		t.Fatalf("no cookies returned from login")
	}
	return cookies[0]
}

// TestChallengerM1_Zero503_ExhaustiveRouteSweep executes an exhaustive route sweep
// across 50+ paths and methods to guarantee ZERO occurrences of HTTP 503
// "This feature is still being converted to MongoDB."
func TestChallengerM1_Zero503_ExhaustiveRouteSweep(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	adminPass := "challenger-pass-zero-503-check"
	cfg := config.Config{
		SigningKey:    strings.Repeat("z", 32),
		AdminPassword: adminPass,
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	cookie := getTestSessionCookie(t, server, adminPass)

	testPaths := []string{
		// Existing Public routes
		"/api/config",
		"/api/v1/health",
		"/api/login",
		"/api/logout",
		"/api/session",
		"/api/checkout",
		"/api/v1/webhooks/paystack",
		"/api/checkout/chk-999/verify",
		"/api/photos/ph-999/download-link",
		"/api/download/ph-999",
		"/api/photos/ph-999/free",
		"/api/photos/ph-999/clean",
		"/api/galleries/gal-999",
		"/api/galleries/gal-999/free-download",
		"/api/galleries/gal-999/qr",
		// Existing Protected routes
		"/api/admin/overview",
		"/api/admin/insights",
		"/api/admin/accounts",
		"/api/admin/activity",
		"/api/admin/account-status",
		"/api/security/password",
		"/api/account/email",
		"/api/settings",
		"/api/galleries",
		"/api/orders",
		"/api/wallet",
		"/api/orders/ord-999/sync",
		"/api/direct-uploads/du-999/complete",
		"/api/photos/ph-999",
		"/api/galleries/gal-999/direct-upload",
		"/api/galleries/gal-999/cover",
		"/api/galleries/gal-999/customization",
		"/api/galleries/gal-999/card-design",
		"/api/galleries/gal-999/print",
		"/api/galleries/gal-999/photos",
		"/api/galleries/gal-999/guest-uploads",
		// Planned Future routes (M2-M6)
		"/api/register",
		"/api/account/verify",
		"/api/account/forgot",
		"/api/account/reset",
		"/api/banks",
		"/api/banks/resolve",
		"/api/withdrawals",
		"/api/withdrawals/ref-999/sync",
		"/api/guest-uploads/gu-999/preview",
		"/api/guest-uploads/gu-999/review",
		"/api/guest-uploads/status",
		"/api/settings/logo",
		"/api/galleries/gal-999/logo",
		"/api/settings/apply-brand",
		"/api/galleries/gal-999/regenerate",
		"/api/admin/sync",
		// Arbitrary Unknown & Edge paths
		"/api/",
		"/api/unknown",
		"/api/does-not-exist",
		"/api/v1/unknown",
		"/api/admin/unknown",
		"/api/galleries/gal-999/unknown",
		"/api/photos/ph-999/unknown",
		"/api/orders/ord-999/unknown",
		"/api/direct-uploads/du-999/unknown",
		"/api/download/",
		"/api/photos/",
		"/api/galleries/",
		"/api/orders/",
		"/api/checkout/",
		"/api/admin/",
		"/api///nested",
		"/api/deeply/nested/nonexistent/path/for/zero503/check",
	}

	methods := []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

	for _, p := range testPaths {
		for _, m := range methods {
			// Test unauthenticated
			t.Run("Unauth "+m+" "+p, func(t *testing.T) {
				r := httptest.NewRequest(m, p, nil)
				w := httptest.NewRecorder()
				server.ServeHTTP(w, r)

				if strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
					t.Fatalf("VIOLATION: route %s %s returned legacy 503 conversion message: %s", m, p, w.Body.String())
				}
				if w.Code == http.StatusServiceUnavailable {
					if !strings.Contains(w.Body.String(), "Direct uploads are not configured.") &&
						!strings.Contains(w.Body.String(), "Paystack is not configured.") &&
						!strings.Contains(p, "/api/v1/health") {
						t.Fatalf("VIOLATION: unexpected 503 for route %s %s: %s", m, p, w.Body.String())
					}
				}
			})

			// Test authenticated
			t.Run("Auth "+m+" "+p, func(t *testing.T) {
				r := httptest.NewRequest(m, p, nil)
				r.AddCookie(cookie)
				w := httptest.NewRecorder()
				server.ServeHTTP(w, r)

				if strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
					t.Fatalf("VIOLATION: route %s %s returned legacy 503 conversion message: %s", m, p, w.Body.String())
				}
				if w.Code == http.StatusServiceUnavailable {
					if !strings.Contains(w.Body.String(), "Direct uploads are not configured.") &&
						!strings.Contains(w.Body.String(), "Paystack is not configured.") &&
						!strings.Contains(p, "/api/v1/health") {
						t.Fatalf("VIOLATION: unexpected 503 for route %s %s: %s", m, p, w.Body.String())
					}
				}
			})
		}
	}
}

// TestChallengerM1_UnknownRoutes_404StrictMatrix tests a matrix of invalid, nonexistent,
// malformed, and out-of-scope paths. Verifies status 404 and clean JSON error response
// for both unauthenticated and authenticated requests.
func TestChallengerM1_UnknownRoutes_404StrictMatrix(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	adminPass := "challenger-pass-unknown-404-matrix"
	cfg := config.Config{
		SigningKey:    strings.Repeat("u", 32),
		AdminPassword: adminPass,
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	cookie := getTestSessionCookie(t, server, adminPass)

	unknownPaths := []string{
		"/api/",
		"/api/nonexistent",
		"/api/foo/bar",
		"/api/v1/unknown",
		"/api/admin/doesnotexist",
		"/api/galleries/gal-123/fake-action",
		"/api/photos/ph-123/fake-action",
		"/api/orders/ord-123/fake-action",
		"/api/direct-uploads/du-123/fake-action",
		"/api/download/",
		"/api/photos/",
		"/api/galleries/",
		"/api/orders/",
		"/api/checkout/",
		"/api/admin/",
		"/api///nested",
		"/api/settings/invalid-subpath",
		"/api/security/invalid-subpath",
		"/api/register",
		"/api/banks",
		"/api/banks/resolve",
		"/api/withdrawals",
		"/api/withdrawals/ref-1/sync",
	}

	for _, p := range unknownPaths {
		for _, auth := range []bool{false, true} {
			name := "Unauth GET " + p
			if auth {
				name = "Auth GET " + p
			}
			t.Run(name, func(t *testing.T) {
				r := httptest.NewRequest("GET", p, nil)
				if auth {
					r.AddCookie(cookie)
				}
				w := httptest.NewRecorder()
				server.ServeHTTP(w, r)

				if w.Code != http.StatusNotFound {
					t.Fatalf("expected HTTP 404 for %s, got %d: %s", p, w.Code, w.Body.String())
				}

				var res map[string]string
				if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
					t.Fatalf("response body is not valid JSON: %v (body: %s)", err, w.Body.String())
				}
				if res["error"] != "Endpoint not found." {
					t.Errorf("expected error 'Endpoint not found.', got '%s'", res["error"])
				}
			})
		}
	}
}

// TestChallengerM1_MethodNotAllowed_ExhaustiveMatrix verifies that every recognized
// endpoint properly returns HTTP 405 Method Not Allowed when accessed with an invalid HTTP method.
func TestChallengerM1_MethodNotAllowed_ExhaustiveMatrix(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	adminPass := "challenger-pass-method-405-matrix"
	cfg := config.Config{
		SigningKey:    strings.Repeat("m", 32),
		AdminPassword: adminPass,
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	cookie := getTestSessionCookie(t, server, adminPass)

	cases := []struct {
		method string
		path   string
		auth   bool
	}{
		// Public endpoints with invalid methods
		{"POST", "/api/config", false},
		{"PUT", "/api/config", false},
		{"DELETE", "/api/config", false},
		{"POST", "/api/v1/health", false},
		{"PUT", "/api/v1/health", false},
		{"DELETE", "/api/v1/health", false},
		{"GET", "/api/login", false},
		{"PUT", "/api/login", false},
		{"DELETE", "/api/login", false},
		{"GET", "/api/logout", false},
		{"PUT", "/api/logout", false},
		{"POST", "/api/session", false},
		{"DELETE", "/api/session", false},
		{"GET", "/api/checkout", false},
		{"PUT", "/api/checkout", false},
		{"GET", "/api/v1/webhooks/paystack", false},
		{"PUT", "/api/v1/webhooks/paystack", false},
		{"GET", "/api/checkout/chk-1/verify", false},
		{"DELETE", "/api/checkout/chk-1/verify", false},
		{"GET", "/api/photos/ph-1/download-link", false},
		{"DELETE", "/api/photos/ph-1/download-link", false},
		{"POST", "/api/download/ph-1", false},
		{"DELETE", "/api/download/ph-1", false},
		{"POST", "/api/photos/ph-1/free", false},
		{"DELETE", "/api/photos/ph-1/free", false},
		{"POST", "/api/photos/ph-1/clean", false},
		{"DELETE", "/api/photos/ph-1/clean", false},
		{"POST", "/api/galleries/gal-1/free-download", false},
		{"DELETE", "/api/galleries/gal-1/free-download", false},
		{"POST", "/api/galleries/gal-1/qr", false},
		{"DELETE", "/api/galleries/gal-1/qr", false},

		// Authenticated endpoints with invalid methods
		{"POST", "/api/admin/overview", true},
		{"DELETE", "/api/admin/overview", true},
		{"POST", "/api/admin/insights", true},
		{"PUT", "/api/admin/accounts", true},
		{"DELETE", "/api/admin/accounts", true},
		{"POST", "/api/admin/activity", true},
		{"GET", "/api/admin/account-status", true},
		{"GET", "/api/security/password", true},
		{"GET", "/api/account/email", true},
		{"DELETE", "/api/settings", true},
		{"PUT", "/api/galleries", true},
		{"DELETE", "/api/galleries", true},
		{"POST", "/api/orders", true},
		{"DELETE", "/api/orders", true},
		{"POST", "/api/wallet", true},
		{"DELETE", "/api/wallet", true},
		{"GET", "/api/orders/ord-1/sync", true},
		{"DELETE", "/api/orders/ord-1/sync", true},
		{"GET", "/api/direct-uploads/du-1/complete", true},
		{"DELETE", "/api/direct-uploads/du-1/complete", true},
		{"POST", "/api/photos/ph-1", true},
		{"GET", "/api/photos/ph-1", true},
		{"GET", "/api/galleries/gal-1/direct-upload", true},
		{"GET", "/api/galleries/gal-1/cover", true},
		{"GET", "/api/galleries/gal-1/customization", true},
		{"GET", "/api/galleries/gal-1/card-design", true},
		{"POST", "/api/galleries/gal-1/print", true},
		{"GET", "/api/galleries/gal-1/photos", true},
		{"PUT", "/api/galleries/gal-1", true},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, nil)
			if tc.auth {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)

			if w.Code != http.StatusMethodNotAllowed {
				t.Fatalf("expected HTTP 405 for %s %s, got %d: %s", tc.method, tc.path, w.Code, w.Body.String())
			}

			var res map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatalf("invalid json response: %v (%s)", err, w.Body.String())
			}
			if res["error"] != "Method not allowed." {
				t.Errorf("expected error 'Method not allowed.', got '%s'", res["error"])
			}
		})
	}
}

// TestChallengerM1_PublicVsAuth_StrictBoundary verifies:
// 1. Public endpoints succeed without any cookie.
// 2. Protected endpoints return 401 with "Please sign in to your studio." when no cookie is present.
// 3. Protected endpoints return 401 with tampered/expired/malformed cookies.
func TestChallengerM1_PublicVsAuth_StrictBoundary(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	adminPass := "challenger-pass-auth-boundary"
	cfg := config.Config{
		SigningKey:    strings.Repeat("b", 32),
		AdminPassword: adminPass,
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	// 1. Verify Public endpoints accessible unauthenticated
	t.Run("Public_Config_Accessible", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/api/config", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Public_Health_Accessible", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/api/v1/health", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
	})

	t.Run("Public_Session_Accessible", func(t *testing.T) {
		r := httptest.NewRequest("GET", "/api/session", nil)
		w := httptest.NewRecorder()
		server.ServeHTTP(w, r)
		if w.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", w.Code)
		}
		var res map[string]any
		_ = json.Unmarshal(w.Body.Bytes(), &res)
		if res["owner"] != false {
			t.Errorf("expected owner: false for unauthenticated caller")
		}
	})

	// 2. Verify Protected endpoints reject unauthenticated callers with 401
	protectedPaths := []struct {
		method string
		path   string
	}{
		{"GET", "/api/admin/overview"},
		{"GET", "/api/admin/insights"},
		{"GET", "/api/admin/accounts"},
		{"POST", "/api/admin/accounts"},
		{"GET", "/api/admin/activity"},
		{"POST", "/api/admin/account-status"},
		{"POST", "/api/security/password"},
		{"POST", "/api/account/email"},
		{"GET", "/api/settings"},
		{"PUT", "/api/settings"},
		{"GET", "/api/galleries"},
		{"POST", "/api/galleries"},
		{"GET", "/api/orders"},
		{"GET", "/api/wallet"},
		{"PATCH", "/api/galleries/gal-1"},
		{"DELETE", "/api/galleries/gal-1"},
		{"POST", "/api/galleries/gal-1/direct-upload"},
		{"PUT", "/api/galleries/gal-1/cover"},
		{"PATCH", "/api/galleries/gal-1/customization"},
		{"PUT", "/api/galleries/gal-1/card-design"},
		{"GET", "/api/galleries/gal-1/print"},
		{"POST", "/api/galleries/gal-1/photos"},
		{"POST", "/api/orders/ord-1/sync"},
		{"POST", "/api/direct-uploads/du-1/complete"},
		{"PATCH", "/api/photos/ph-1"},
		{"DELETE", "/api/photos/ph-1"},
	}

	for _, p := range protectedPaths {
		t.Run("UnauthReject "+p.method+" "+p.path, func(t *testing.T) {
			r := httptest.NewRequest(p.method, p.path, nil)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected HTTP 401 for %s %s, got %d: %s", p.method, p.path, w.Code, w.Body.String())
			}

			var res map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
				t.Fatalf("invalid json: %v", err)
			}
			if res["error"] != "Please sign in to your studio." {
				t.Errorf("expected 'Please sign in to your studio.', got '%s'", res["error"])
			}
		})
	}

	// 3. Verify Tampered and Expired Session Cookies
	tamperedCookies := []*http.Cookie{
		{Name: "lumen_platform", Value: "malformed_no_dots"},
		{Name: "lumen_platform", Value: "part1.part2.part3"}, // only 3 parts
		{Name: "lumen_platform", Value: "admin.12345.v1.invalidsig"}, // invalid sig
		{Name: "lumen_platform", Value: "admin.100.v1.sig"}, // expired timestamp (year 1970)
	}

	for i, tc := range tamperedCookies {
		t.Run("TamperedCookie_"+string(rune('A'+i)), func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/admin/overview", nil)
			r.AddCookie(tc)
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)

			if w.Code != http.StatusUnauthorized {
				t.Fatalf("expected HTTP 401 for tampered cookie, got %d: %s", w.Code, w.Body.String())
			}
		})
	}
}

// TestChallengerM1_ConcurrentRouterStress hammers the router under high concurrent load
// with mixed valid, invalid, auth, and public requests to detect data races or router deadlocks.
func TestChallengerM1_ConcurrentRouterStress(t *testing.T) {
	_, db, cleanup := setupM1TestDB(t)
	defer cleanup()

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()

	adminPass := "challenger-pass-concurrent-load"
	cfg := config.Config{
		SigningKey:    strings.Repeat("l", 32),
		AdminPassword: adminPass,
	}

	server, err := New(ctx, db, cfg)
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	cookie := getTestSessionCookie(t, server, adminPass)

	const workerCount = 100
	var wg sync.WaitGroup
	var completedCount int64
	var violationCount int64

	routes := []struct {
		method string
		path   string
		auth   bool
	}{
		{"GET", "/api/config", false},
		{"GET", "/api/v1/health", false},
		{"GET", "/api/session", false},
		{"GET", "/api/session", true},
		{"GET", "/api/admin/overview", true},
		{"GET", "/api/admin/overview", false},
		{"GET", "/api/unknown-concurrent-1", false},
		{"GET", "/api/unknown-concurrent-2", true},
		{"POST", "/api/config", false}, // 405
		{"DELETE", "/api/v1/health", false}, // 405
		{"POST", "/api/wallet", true}, // 405
	}

	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			rInfo := routes[idx%len(routes)]
			r := httptest.NewRequest(rInfo.method, rInfo.path, nil)
			if rInfo.auth {
				r.AddCookie(cookie)
			}
			w := httptest.NewRecorder()
			server.ServeHTTP(w, r)

			if strings.Contains(w.Body.String(), "This feature is still being converted to MongoDB.") {
				atomic.AddInt64(&violationCount, 1)
			}
			if w.Code == http.StatusInternalServerError {
				atomic.AddInt64(&violationCount, 1)
			}
			atomic.AddInt64(&completedCount, 1)
		}(i)
	}

	wg.Wait()

	if violationCount > 0 {
		t.Fatalf("detected %d violations (503 stubs or 500 internal errors) under concurrent load", violationCount)
	}
	if completedCount != workerCount {
		t.Errorf("expected %d completed requests, got %d", workerCount, completedCount)
	}
}
