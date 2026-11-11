# Strategy: Database Schemas, BSON Models & Index Provisioning (Milestone M1)

## 1. Executive Summary

Milestone M1 establishes the core MongoDB data layer and startup index provisioning for Pictide Studio. 
This document defines the exact architecture, BSON schema structures, collection repository helpers, and index provisioning plan to be implemented by the Worker agent.

The scope covers:
1. **Four new domain stores and BSON models** in `internal/mongostore/`:
   - `withdrawals.go` (`withdrawals` collection)
   - `guest_uploads.go` (`guest_uploads` collection)
   - `account_links.go` (`account_links` collection)
   - `admin_audit.go` (`admin_audit` collection)
2. **Index provisioning at application startup** in `httpapi.New` (`internal/httpapi/server.go`).
3. **Data contract compatibility**: Full alignment with `PROJECT.md` interface contracts, existing handlers (`wallet.go`, `admin_activity.go`, `insights.go`, `workspace.go`), and frontend consumers (`public/auth-admin.js`, `public/admin-console.js`, `public/enhancements.js`).
4. **Automated unit tests** in `internal/mongostore/models_test.go` to verify BSON encoding/decoding and store helper contracts without external dependencies.

---

## 2. Architectural Analysis & Constraints

### 2.1 Multi-Tenant Isolation
Pictide enforces multi-tenant isolation keyed on `studio_id`.
- `galleries`, `photos`, `orders`, `withdrawals`, and `guest_uploads` partition records by `studio_id`.
- In `withdrawals`, public webhooks from Paystack only supply `reference`, so query patterns require indexing both `{studio_id: 1, created_at: -1}` and `{reference: 1}`.
- In `guest_uploads`, submissions belong to a gallery and a studio, with guest tracking performed via tokens or gallery lookup, requiring indexes on `{gallery_id: 1, created_at: -1}` and `{studio_id: 1}`.

### 2.2 Timestamp Alignment
Existing codebases use two distinct timestamp conventions:
1. String timestamps: RFC3339 formatted strings (e.g., `timestamp()` in `server.go`), used by legacy overview endpoints and frontend formatting functions like `date(x.created)`.
2. Integer Unix timestamps: `int64` epoch seconds (`created_at`, `updated_at`), specified in `PROJECT.md` lines 63–67 and used for compound sorting indexes (`{studio_id: 1, created_at: -1}`).
3. Native BSON Dates: `time.Time` for TTL indexes (e.g., `expires_at` in `account_links` and `direct_uploads`).

**Strategy**: The BSON models bridge both conventions:
- Primary contract fields: `created_at int64`, `updated_at int64`, `expires_at time.Time`.
- Compatibility fields: `created string bson:"created,omitempty"` to preserve backward compatibility with existing queries in `admin_activity.go`, `wallet.go`, and `insights.go` that decode `created` as a string.

---

## 3. Data Schema Specifications & BSON Models

### 3.1 `internal/mongostore/withdrawals.go`
- **Collection**: `withdrawals`
- **Contract Reference**: `PROJECT.md:65`
- **Required Indexes**:
  - `{id: 1}` (unique)
  - `{studio_id: 1, created_at: -1}`
  - `{reference: 1}`

#### BSON Model:
```go
type Withdrawal struct {
	ID            string `bson:"id" json:"id"`
	StudioID      string `bson:"studio_id" json:"studio_id"`
	Amount        int64  `bson:"amount" json:"amount"`
	Fee           int64  `bson:"fee" json:"fee"`
	Currency      string `bson:"currency" json:"currency"`
	RecipientCode string `bson:"recipient_code" json:"recipient_code"`
	Reference     string `bson:"reference" json:"reference"`
	Status        string `bson:"status" json:"status"` // "pending", "otp", "success", "failed", "reversed"
	BankCode      string `bson:"bank_code" json:"bank_code"`
	Bank          string `bson:"bank,omitempty" json:"bank,omitempty"`
	AccountNumber string `bson:"account_number" json:"account_number"`
	AccountName   string `bson:"account_name" json:"account_name"`
	AccountLast4  string `bson:"account_last4,omitempty" json:"account_last4,omitempty"`
	CreatedAt     int64  `bson:"created_at" json:"created_at"`
	UpdatedAt     int64  `bson:"updated_at" json:"updated_at"`
	Created       string `bson:"created,omitempty" json:"created,omitempty"`
}
```

#### Collection Helper Methods:
```go
type Withdrawals struct {
	collection *mongo.Collection
}

func NewWithdrawals(db *mongo.Database) (*Withdrawals, error)
func (w *Withdrawals) EnsureIndexes(ctx context.Context) error
func (w *Withdrawals) Create(ctx context.Context, item *Withdrawal) error
func (w *Withdrawals) GetByID(ctx context.Context, id string) (*Withdrawal, error)
func (w *Withdrawals) GetByReference(ctx context.Context, reference string) (*Withdrawal, error)
func (w *Withdrawals) ListByStudio(ctx context.Context, studioID string, limit int64) ([]Withdrawal, error)
func (w *Withdrawals) ListAll(ctx context.Context, limit int64) ([]Withdrawal, error)
func (w *Withdrawals) UpdateStatus(ctx context.Context, reference string, status string, updatedAt int64) error
func (w *Withdrawals) CountPendingByStudio(ctx context.Context, studioID string) (int64, error)
```

---

### 3.2 `internal/mongostore/guest_uploads.go`
- **Collection**: `guest_uploads`
- **Contract Reference**: `PROJECT.md:66`
- **Required Indexes**:
  - `{id: 1}` (unique)
  - `{gallery_id: 1, created_at: -1}`
  - `{studio_id: 1}`

#### BSON Model:
```go
type GuestUpload struct {
	ID        string        `bson:"id" json:"id"`
	StudioID  string        `bson:"studio_id" json:"studio_id"`
	GalleryID string        `bson:"gallery_id" json:"gallery_id"`
	GuestName string        `bson:"guest_name" json:"guest_name"`
	Filename  string        `bson:"filename" json:"filename"`
	Name      string        `bson:"name,omitempty" json:"name,omitempty"` // Alias for frontend compatibility (enhancements.js uses p.name)
	Size      int64         `bson:"size" json:"size"`
	Status    string        `bson:"status" json:"status"` // "pending", "approved", "rejected"
	FileID    bson.ObjectID `bson:"file_id,omitempty" json:"file_id,omitempty"` // GridFS chunk reference
	CreatedAt int64         `bson:"created_at" json:"created_at"`
	Created   string        `bson:"created,omitempty" json:"created,omitempty"`
}
```

#### Collection Helper Methods:
```go
type GuestUploads struct {
	collection *mongo.Collection
}

func NewGuestUploads(db *mongo.Database) (*GuestUploads, error)
func (g *GuestUploads) EnsureIndexes(ctx context.Context) error
func (g *GuestUploads) Create(ctx context.Context, item *GuestUpload) error
func (g *GuestUploads) GetByID(ctx context.Context, id string) (*GuestUpload, error)
func (g *GuestUploads) ListByGallery(ctx context.Context, galleryID string) ([]GuestUpload, error)
func (g *GuestUploads) ListByStudio(ctx context.Context, studioID string) ([]GuestUpload, error)
func (g *GuestUploads) ListByIDs(ctx context.Context, ids []string) ([]GuestUpload, error)
func (g *GuestUploads) UpdateStatus(ctx context.Context, id string, status string) error
func (g *GuestUploads) CountPending(ctx context.Context) (int64, error)
```

---

### 3.3 `internal/mongostore/account_links.go`
- **Collection**: `account_links`
- **Contract Reference**: `PROJECT.md:64`
- **Required Indexes**:
  - `{token: 1}` (unique)
  - `{expires_at: 1}` (TTL index, expireAfterSeconds: 0)

#### BSON Model:
```go
type AccountLink struct {
	Token     string    `bson:"token" json:"token"`
	AccountID string    `bson:"account_id" json:"account_id"`
	Purpose   string    `bson:"purpose" json:"purpose"` // e.g., "verify_email", "password_reset"
	ExpiresAt time.Time `bson:"expires_at" json:"expires_at"`
	CreatedAt time.Time `bson:"created_at,omitempty" json:"created_at,omitempty"`
}
```

#### Collection Helper Methods:
```go
type AccountLinks struct {
	collection *mongo.Collection
}

func NewAccountLinks(db *mongo.Database) (*AccountLinks, error)
func (a *AccountLinks) EnsureIndexes(ctx context.Context) error
func (a *AccountLinks) Create(ctx context.Context, item *AccountLink) error
func (a *AccountLinks) GetByToken(ctx context.Context, token string) (*AccountLink, error)
func (a *AccountLinks) GetValid(ctx context.Context, token string, purpose string) (*AccountLink, error)
func (a *AccountLinks) DeleteByToken(ctx context.Context, token string) error
func (a *AccountLinks) DeleteByAccountAndPurpose(ctx context.Context, accountID string, purpose string) error
```

---

### 3.4 `internal/mongostore/admin_audit.go`
- **Collection**: `admin_audit`
- **Contract Reference**: `PROJECT.md:67`
- **Required Indexes**:
  - `{id: 1}` (unique)
  - `{created_at: -1}`

#### BSON Model:
```go
type AdminAuditEvent struct {
	ID        string `bson:"id" json:"id"`
	Actor     string `bson:"actor,omitempty" json:"actor,omitempty"`
	Action    string `bson:"action" json:"action"`
	Target    string `bson:"target,omitempty" json:"target,omitempty"`
	Details   string `bson:"details,omitempty" json:"details,omitempty"`
	CreatedAt int64  `bson:"created_at" json:"created_at"`
	Created   string `bson:"created,omitempty" json:"created,omitempty"`
}
```

#### Collection Helper Methods:
```go
type AdminAudit struct {
	collection *mongo.Collection
}

func NewAdminAudit(db *mongo.Database) (*AdminAudit, error)
func (a *AdminAudit) EnsureIndexes(ctx context.Context) error
func (a *AdminAudit) Record(ctx context.Context, event *AdminAuditEvent) error
func (a *AdminAudit) ListRecent(ctx context.Context, limit int64) ([]AdminAuditEvent, error)
```

---

## 4. Index Provisioning in `httpapi.New` (`server.go`)

### 4.1 Required Startup Provisioning
In `internal/httpapi/server.go`, `httpapi.New(ctx, db, cfg)` runs at application startup and before handling any requests. 

The existing index setup in lines 55–79 provisions:
- `accounts`: `{username: 1}` (unique)
- `galleries`: `{id: 1}` (unique)
- `photos`: `{id: 1}` (unique), `{studio_id: 1, gallery_id: 1, hash: 1}` (unique)
- `orders`: `{id: 1}` (unique)
- `direct_uploads` (if R2 enabled): `{expires_at: 1}` (TTL index, expireAfterSeconds: 0)

### 4.2 Exact Indexes to Add in `httpapi.New`
1. **`accounts`**:
   - `{username: 1}` (unique) — already present.
   - `{email: 1}` (sparse) — **ADD**:
     ```go
     mongo.IndexModel{
         Keys:    bson.D{{Key: "email", Value: 1}},
         Options: options.Index().SetSparse(true),
     }
     ```
2. **`account_links`**:
   - `{token: 1}` (unique) — **ADD**:
     ```go
     mongo.IndexModel{
         Keys:    bson.D{{Key: "token", Value: 1}},
         Options: options.Index().SetUnique(true),
     }
     ```
   - `{expires_at: 1}` (TTL index, expireAfterSeconds: 0) — **ADD**:
     ```go
     mongo.IndexModel{
         Keys:    bson.D{{Key: "expires_at", Value: 1}},
         Options: options.Index().SetExpireAfterSeconds(0),
     }
     ```
3. **`withdrawals`**:
   - `{id: 1}` (unique) — **ADD**
   - `{studio_id: 1, created_at: -1}` — **ADD**
   - `{reference: 1}` — **ADD**
4. **`guest_uploads`**:
   - `{id: 1}` (unique) — **ADD**
   - `{gallery_id: 1, created_at: -1}` — **ADD**
   - `{studio_id: 1}` — **ADD**
5. **`admin_audit`**:
   - `{id: 1}` (unique) — **ADD**
   - `{created_at: -1}` — **ADD**

### 4.3 Update to `account` Struct in `server.go`
In `server.go` (lines 37–45):
```go
type account struct {
	ID           string `bson:"_id"`
	Username     string `bson:"username"`
	Email        string `bson:"email,omitempty"`
	Verified     bool   `bson:"verified"`
	PasswordHash string `bson:"password_hash"`
	Role         string `bson:"role"`
	Version      string `bson:"session_version"`
	Disabled     bool   `bson:"disabled"`
	Created      string `bson:"created"`
}
```
Adding `Email` and `Verified` allows session decoding and account operations in M2 and M6 to read and persist contact and verification fields without breaking legacy admin records.

---

## 5. Detailed Code Diff Plan for the Worker

### Diff 1: Create `internal/mongostore/withdrawals.go`
```go
package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// Withdrawal represents a payout request or transaction record.
type Withdrawal struct {
	ID            string `bson:"id" json:"id"`
	StudioID      string `bson:"studio_id" json:"studio_id"`
	Amount        int64  `bson:"amount" json:"amount"`
	Fee           int64  `bson:"fee" json:"fee"`
	Currency      string `bson:"currency" json:"currency"`
	RecipientCode string `bson:"recipient_code" json:"recipient_code"`
	Reference     string `bson:"reference" json:"reference"`
	Status        string `bson:"status" json:"status"` // "pending", "otp", "success", "failed", "reversed"
	BankCode      string `bson:"bank_code" json:"bank_code"`
	Bank          string `bson:"bank,omitempty" json:"bank,omitempty"`
	AccountNumber string `bson:"account_number" json:"account_number"`
	AccountName   string `bson:"account_name" json:"account_name"`
	AccountLast4  string `bson:"account_last4,omitempty" json:"account_last4,omitempty"`
	CreatedAt     int64  `bson:"created_at" json:"created_at"`
	UpdatedAt     int64  `bson:"updated_at" json:"updated_at"`
	Created       string `bson:"created,omitempty" json:"created,omitempty"`
}

type Withdrawals struct {
	collection *mongo.Collection
}

func NewWithdrawals(db *mongo.Database) (*Withdrawals, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &Withdrawals{collection: db.Collection("withdrawals")}, nil
}

func (w *Withdrawals) EnsureIndexes(ctx context.Context) error {
	_, err := w.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "reference", Value: 1}}},
	})
	return err
}

func (w *Withdrawals) Create(ctx context.Context, item *Withdrawal) error {
	if item == nil || item.ID == "" || item.StudioID == "" || item.Reference == "" {
		return errors.New("invalid withdrawal record")
	}
	if item.CreatedAt == 0 {
		item.CreatedAt = time.Now().Unix()
	}
	if item.UpdatedAt == 0 {
		item.UpdatedAt = item.CreatedAt
	}
	if item.Created == "" {
		item.Created = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if item.AccountLast4 == "" && len(item.AccountNumber) >= 4 {
		item.AccountLast4 = item.AccountNumber[len(item.AccountNumber)-4:]
	}
	_, err := w.collection.InsertOne(ctx, item)
	return err
}

func (w *Withdrawals) GetByID(ctx context.Context, id string) (*Withdrawal, error) {
	if id == "" {
		return nil, errors.New("withdrawal id required")
	}
	var out Withdrawal
	err := w.collection.FindOne(ctx, bson.M{"id": id}).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (w *Withdrawals) GetByReference(ctx context.Context, reference string) (*Withdrawal, error) {
	if reference == "" {
		return nil, errors.New("withdrawal reference required")
	}
	var out Withdrawal
	err := w.collection.FindOne(ctx, bson.M{"reference": reference}).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (w *Withdrawals) ListByStudio(ctx context.Context, studioID string, limit int64) ([]Withdrawal, error) {
	if studioID == "" {
		return nil, errors.New("studio id required")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	cur, err := w.collection.Find(ctx, bson.M{"studio_id": studioID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []Withdrawal
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (w *Withdrawals) ListAll(ctx context.Context, limit int64) ([]Withdrawal, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	cur, err := w.collection.Find(ctx, bson.M{}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []Withdrawal
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (w *Withdrawals) UpdateStatus(ctx context.Context, reference string, status string, updatedAt int64) error {
	if reference == "" || status == "" {
		return errors.New("reference and status required")
	}
	if updatedAt == 0 {
		updatedAt = time.Now().Unix()
	}
	res, err := w.collection.UpdateOne(ctx, bson.M{"reference": reference}, bson.M{"$set": bson.M{"status": status, "updated_at": updatedAt}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (w *Withdrawals) CountPendingByStudio(ctx context.Context, studioID string) (int64, error) {
	if studioID == "" {
		return 0, errors.New("studio id required")
	}
	return w.collection.CountDocuments(ctx, bson.M{"studio_id": studioID, "status": bson.M{"$in": bson.A{"pending", "otp"}}})
}
```

---

### Diff 2: Create `internal/mongostore/guest_uploads.go`
```go
package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// GuestUpload represents a photo contributed by a gallery guest.
type GuestUpload struct {
	ID        string        `bson:"id" json:"id"`
	StudioID  string        `bson:"studio_id" json:"studio_id"`
	GalleryID string        `bson:"gallery_id" json:"gallery_id"`
	GuestName string        `bson:"guest_name" json:"guest_name"`
	Filename  string        `bson:"filename" json:"filename"`
	Name      string        `bson:"name,omitempty" json:"name,omitempty"`
	Size      int64         `bson:"size" json:"size"`
	Status    string        `bson:"status" json:"status"` // "pending", "approved", "rejected"
	FileID    bson.ObjectID `bson:"file_id,omitempty" json:"file_id,omitempty"`
	CreatedAt int64         `bson:"created_at" json:"created_at"`
	Created   string        `bson:"created,omitempty" json:"created,omitempty"`
}

type GuestUploads struct {
	collection *mongo.Collection
}

func NewGuestUploads(db *mongo.Database) (*GuestUploads, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &GuestUploads{collection: db.Collection("guest_uploads")}, nil
}

func (g *GuestUploads) EnsureIndexes(ctx context.Context) error {
	_, err := g.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "gallery_id", Value: 1}, {Key: "created_at", Value: -1}}},
		{Keys: bson.D{{Key: "studio_id", Value: 1}}},
	})
	return err
}

func (g *GuestUploads) Create(ctx context.Context, item *GuestUpload) error {
	if item == nil || item.ID == "" || item.StudioID == "" || item.GalleryID == "" {
		return errors.New("invalid guest upload record")
	}
	if item.CreatedAt == 0 {
		item.CreatedAt = time.Now().Unix()
	}
	if item.Created == "" {
		item.Created = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if item.Name == "" {
		item.Name = item.Filename
	}
	if item.Status == "" {
		item.Status = "pending"
	}
	_, err := g.collection.InsertOne(ctx, item)
	return err
}

func (g *GuestUploads) GetByID(ctx context.Context, id string) (*GuestUpload, error) {
	if id == "" {
		return nil, errors.New("upload id required")
	}
	var out GuestUpload
	err := g.collection.FindOne(ctx, bson.M{"id": id}).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (g *GuestUploads) ListByGallery(ctx context.Context, galleryID string) ([]GuestUpload, error) {
	if galleryID == "" {
		return nil, errors.New("gallery id required")
	}
	cur, err := g.collection.Find(ctx, bson.M{"gallery_id": galleryID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []GuestUpload
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (g *GuestUploads) ListByStudio(ctx context.Context, studioID string) ([]GuestUpload, error) {
	if studioID == "" {
		return nil, errors.New("studio id required")
	}
	cur, err := g.collection.Find(ctx, bson.M{"studio_id": studioID}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []GuestUpload
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (g *GuestUploads) ListByIDs(ctx context.Context, ids []string) ([]GuestUpload, error) {
	if len(ids) == 0 {
		return []GuestUpload{}, nil
	}
	cur, err := g.collection.Find(ctx, bson.M{"id": bson.M{"$in": ids}}, options.Find().SetSort(bson.D{{Key: "created_at", Value: -1}}))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []GuestUpload
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}

func (g *GuestUploads) UpdateStatus(ctx context.Context, id string, status string) error {
	if id == "" || status == "" {
		return errors.New("id and status required")
	}
	res, err := g.collection.UpdateOne(ctx, bson.M{"id": id}, bson.M{"$set": bson.M{"status": status}})
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return mongo.ErrNoDocuments
	}
	return nil
}

func (g *GuestUploads) CountPending(ctx context.Context) (int64, error) {
	return g.collection.CountDocuments(ctx, bson.M{"status": "pending"})
}
```

---

### Diff 3: Create `internal/mongostore/account_links.go`
```go
package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AccountLink represents an ephemeral verification or password reset token.
type AccountLink struct {
	Token     string    `bson:"token" json:"token"`
	AccountID string    `bson:"account_id" json:"account_id"`
	Purpose   string    `bson:"purpose" json:"purpose"` // "verify_email", "password_reset"
	ExpiresAt time.Time `bson:"expires_at" json:"expires_at"`
	CreatedAt time.Time `bson:"created_at,omitempty" json:"created_at,omitempty"`
}

type AccountLinks struct {
	collection *mongo.Collection
}

func NewAccountLinks(db *mongo.Database) (*AccountLinks, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &AccountLinks{collection: db.Collection("account_links")}, nil
}

func (a *AccountLinks) EnsureIndexes(ctx context.Context) error {
	_, err := a.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "token", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
	})
	return err
}

func (a *AccountLinks) Create(ctx context.Context, item *AccountLink) error {
	if item == nil || item.Token == "" || item.AccountID == "" || item.Purpose == "" || item.ExpiresAt.IsZero() {
		return errors.New("invalid account link record")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = time.Now()
	}
	_, err := a.collection.InsertOne(ctx, item)
	return err
}

func (a *AccountLinks) GetByToken(ctx context.Context, token string) (*AccountLink, error) {
	if token == "" {
		return nil, errors.New("token required")
	}
	var out AccountLink
	err := a.collection.FindOne(ctx, bson.M{"token": token}).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *AccountLinks) GetValid(ctx context.Context, token string, purpose string) (*AccountLink, error) {
	if token == "" || purpose == "" {
		return nil, errors.New("token and purpose required")
	}
	var out AccountLink
	filter := bson.M{
		"token":      token,
		"purpose":    purpose,
		"expires_at": bson.M{"$gt": time.Now()},
	}
	err := a.collection.FindOne(ctx, filter).Decode(&out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

func (a *AccountLinks) DeleteByToken(ctx context.Context, token string) error {
	if token == "" {
		return errors.New("token required")
	}
	_, err := a.collection.DeleteOne(ctx, bson.M{"token": token})
	return err
}

func (a *AccountLinks) DeleteByAccountAndPurpose(ctx context.Context, accountID string, purpose string) error {
	if accountID == "" || purpose == "" {
		return errors.New("account id and purpose required")
	}
	_, err := a.collection.DeleteMany(ctx, bson.M{"account_id": accountID, "purpose": purpose})
	return err
}
```

---

### Diff 4: Create `internal/mongostore/admin_audit.go`
```go
package mongostore

import (
	"context"
	"errors"
	"time"

	"go.mongodb.org/mongo-driver/v2/bson"
	"go.mongodb.org/mongo-driver/v2/mongo"
	"go.mongodb.org/mongo-driver/v2/mongo/options"
)

// AdminAuditEvent represents an administrative action or access event.
type AdminAuditEvent struct {
	ID        string `bson:"id" json:"id"`
	Actor     string `bson:"actor,omitempty" json:"actor,omitempty"`
	Action    string `bson:"action" json:"action"`
	Target    string `bson:"target,omitempty" json:"target,omitempty"`
	Details   string `bson:"details,omitempty" json:"details,omitempty"`
	CreatedAt int64  `bson:"created_at" json:"created_at"`
	Created   string `bson:"created,omitempty" json:"created,omitempty"`
}

type AdminAudit struct {
	collection *mongo.Collection
}

func NewAdminAudit(db *mongo.Database) (*AdminAudit, error) {
	if db == nil {
		return nil, errors.New("database is required")
	}
	return &AdminAudit{collection: db.Collection("admin_audit")}, nil
}

func (a *AdminAudit) EnsureIndexes(ctx context.Context) error {
	_, err := a.collection.Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
		{Keys: bson.D{{Key: "created_at", Value: -1}}},
	})
	return err
}

func (a *AdminAudit) Record(ctx context.Context, event *AdminAuditEvent) error {
	if event == nil || event.ID == "" || event.Action == "" {
		return errors.New("invalid audit event record")
	}
	if event.CreatedAt == 0 {
		event.CreatedAt = time.Now().Unix()
	}
	if event.Created == "" {
		event.Created = time.Now().UTC().Format(time.RFC3339Nano)
	}
	_, err := a.collection.InsertOne(ctx, event)
	return err
}

func (a *AdminAudit) ListRecent(ctx context.Context, limit int64) ([]AdminAuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	cur, err := a.collection.Find(ctx, bson.M{}, options.Find().SetProjection(bson.M{"_id": 0}).SetSort(bson.D{{Key: "created_at", Value: -1}}).SetLimit(limit))
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var list []AdminAuditEvent
	if err := cur.All(ctx, &list); err != nil {
		return nil, err
	}
	return list, nil
}
```

---

### Diff 5: Update `internal/httpapi/server.go`
#### Chunk 1: Update `account` struct (lines 37–45)
```diff
--- a/internal/httpapi/server.go
+++ b/internal/httpapi/server.go
@@ -37,6 +37,8 @@ type attempt struct {
 type account struct {
 	ID           string `bson:"_id"`
 	Username     string `bson:"username"`
+	Email        string `bson:"email,omitempty"`
+	Verified     bool   `bson:"verified"`
 	PasswordHash string `bson:"password_hash"`
 	Role         string `bson:"role"`
 	Version      string `bson:"session_version"`
```

#### Chunk 2: Update Index Provisioning in `httpapi.New` (lines 55–80)
```diff
--- a/internal/httpapi/server.go
+++ b/internal/httpapi/server.go
@@ -55,6 +55,14 @@ func New(ctx context.Context, db *mongo.Database, cfg config.Config) (*Server,
-	_, err = db.Collection("accounts").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)})
+	_, err = db.Collection("accounts").Indexes().CreateMany(ctx, []mongo.IndexModel{
+		{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)},
+		{Keys: bson.D{{Key: "email", Value: 1}}, Options: options.Index().SetSparse(true)},
+	})
 	if err != nil {
 		return nil, err
 	}
+	_, err = db.Collection("account_links").Indexes().CreateMany(ctx, []mongo.IndexModel{
+		{Keys: bson.D{{Key: "token", Value: 1}}, Options: options.Index().SetUnique(true)},
+		{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)},
+	})
+	if err != nil {
+		return nil, err
+	}
 	_, err = db.Collection("galleries").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)})
 	if err != nil {
 		return nil, err
 	}
 	_, err = db.Collection("photos").Indexes().CreateMany(ctx, []mongo.IndexModel{
 		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
 		{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "gallery_id", Value: 1}, {Key: "hash", Value: 1}}, Options: options.Index().SetUnique(true)},
 	})
 	if err != nil {
 		return nil, err
 	}
 	_, err = db.Collection("orders").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)})
 	if err != nil {
 		return nil, err
 	}
+	_, err = db.Collection("withdrawals").Indexes().CreateMany(ctx, []mongo.IndexModel{
+		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
+		{Keys: bson.D{{Key: "studio_id", Value: 1}, {Key: "created_at", Value: -1}}},
+		{Keys: bson.D{{Key: "reference", Value: 1}}},
+	})
+	if err != nil {
+		return nil, err
+	}
+	_, err = db.Collection("guest_uploads").Indexes().CreateMany(ctx, []mongo.IndexModel{
+		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
+		{Keys: bson.D{{Key: "gallery_id", Value: 1}, {Key: "created_at", Value: -1}}},
+		{Keys: bson.D{{Key: "studio_id", Value: 1}}},
+	})
+	if err != nil {
+		return nil, err
+	}
+	_, err = db.Collection("admin_audit").Indexes().CreateMany(ctx, []mongo.IndexModel{
+		{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)},
+		{Keys: bson.D{{Key: "created_at", Value: -1}}},
+	})
+	if err != nil {
+		return nil, err
+	}
 	if r2 != nil {
 		_, err = db.Collection("direct_uploads").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)})
 		if err != nil {
 			return nil, err
 		}
 	}
```

---

### Diff 6: Create `internal/mongostore/models_test.go`
A clean unit test file that verifies:
- BSON encoding and decoding roundtrip for each new model
- Proper BSON field tags (`id`, `created_at`, `account_last4`, etc.)
- Constructor validation (rejection of nil database)
- Verification without requiring live MongoDB connection

```go
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
```

---

## 6. Verification and Validation Method

1. **Compilation Check**:
   ```powershell
   go build ./...
   ```
   Ensures all packages compile with zero syntax or type errors.

2. **Unit Tests (mongostore and httpapi)**:
   ```powershell
   go test -v ./internal/mongostore/...
   go test -v ./internal/httpapi/...
   ```
   Ensures all BSON models marshal, unmarshal, validate, and existing tests in `httpapi` pass.

3. **Frontend Suite**:
   ```powershell
   node --test test/*.test.js
   ```
   Ensures no client-side assumptions were invalidated.

4. **Integration Verification (if MongoDB available)**:
   ```powershell
   $env:PICTIDE_TEST_MONGODB="1"; go test -v ./internal/httpapi -run TestMongoWorkspace
   ```
   Ensures `httpapi.New` successfully provisions all indexes on a live test database without error.
