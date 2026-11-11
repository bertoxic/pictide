package mongostore_test

import (
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"pictide.local/studio/internal/mongostore"
)

func TestWithdrawalBSONRoundtrip(t *testing.T) {
	orig := mongostore.Withdrawal{
		ID:            "w_123",
		StudioID:      "studio_abc",
		Amount:        500000,
		Fee:           7500,
		Currency:      "NGN",
		RecipientCode: "RCP_xyz",
		Reference:     "ref_789",
		Status:        "pending",
		BankCode:      "058",
		Bank:          "GTBank",
		AccountNumber: "0123456789",
		AccountName:   "Jane Doe",
		AccountLast4:  "6789",
		CreatedAt:     1700000000,
		UpdatedAt:     1700000010,
		Created:       "2026-11-10T12:00:00Z",
	}

	data, err := bson.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded mongostore.Withdrawal
	if err := bson.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != orig.ID || decoded.StudioID != orig.StudioID || decoded.Amount != orig.Amount {
		t.Fatalf("Field mismatch: %+v vs %+v", decoded, orig)
	}
	if decoded.Reference != orig.Reference || decoded.AccountLast4 != orig.AccountLast4 {
		t.Fatalf("Reference or AccountLast4 mismatch")
	}
}

func TestGuestUploadBSONRoundtrip(t *testing.T) {
	oid := bson.NewObjectID()
	orig := mongostore.GuestUpload{
		ID:        "gu_123",
		StudioID:  "studio_abc",
		GalleryID: "gal_456",
		GuestName: "John Guest",
		Filename:  "photo.jpg",
		Name:      "photo.jpg",
		Size:      102400,
		Status:    "pending",
		FileID:    oid,
		CreatedAt: 1700000000,
		Created:   "2026-11-10T12:00:00Z",
	}

	data, err := bson.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded mongostore.GuestUpload
	if err := bson.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != orig.ID || decoded.FileID != orig.FileID || decoded.Status != orig.Status {
		t.Fatalf("Field mismatch: %+v vs %+v", decoded, orig)
	}
}

func TestAccountLinkBSONRoundtrip(t *testing.T) {
	now := time.Now().Truncate(time.Millisecond)
	orig := mongostore.AccountLink{
		Token:     "tok_abc123",
		AccountID: "acc_999",
		Purpose:   "verify_email",
		ExpiresAt: now.Add(24 * time.Hour),
		CreatedAt: now,
	}

	data, err := bson.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded mongostore.AccountLink
	if err := bson.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.Token != orig.Token || decoded.Purpose != orig.Purpose {
		t.Fatalf("Field mismatch: %+v vs %+v", decoded, orig)
	}
	if !decoded.ExpiresAt.Equal(orig.ExpiresAt) {
		t.Fatalf("Time mismatch: %v vs %v", decoded.ExpiresAt, orig.ExpiresAt)
	}
}

func TestAdminAuditBSONRoundtrip(t *testing.T) {
	orig := mongostore.AdminAuditEvent{
		ID:        "audit_001",
		Actor:     "admin",
		Action:    "viewed account activity",
		Target:    "photographer1",
		Details:   "accessed overview tab",
		CreatedAt: 1700000000,
		Created:   "2026-11-10T12:00:00Z",
	}

	data, err := bson.Marshal(orig)
	if err != nil {
		t.Fatalf("Marshal failed: %v", err)
	}

	var decoded mongostore.AdminAuditEvent
	if err := bson.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	if decoded.ID != orig.ID || decoded.Actor != orig.Actor || decoded.Target != orig.Target {
		t.Fatalf("Field mismatch: %+v vs %+v", decoded, orig)
	}
}

func TestStoreConstructorsRequireDB(t *testing.T) {
	if _, err := mongostore.NewWithdrawals(nil); err == nil {
		t.Fatal("expected error for nil db in NewWithdrawals")
	}
	if _, err := mongostore.NewGuestUploads(nil); err == nil {
		t.Fatal("expected error for nil db in NewGuestUploads")
	}
	if _, err := mongostore.NewAccountLinks(nil); err == nil {
		t.Fatal("expected error for nil db in NewAccountLinks")
	}
	if _, err := mongostore.NewAdminAudit(nil); err == nil {
		t.Fatal("expected error for nil db in NewAdminAudit")
	}
}
