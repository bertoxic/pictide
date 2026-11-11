# Handoff Report: Schemas & Indexes Strategy (Milestone M1)

## 1. Observation
1. **Existing `internal/mongostore/` layout**:
   Inspected directory `c:\Users\Hp\Desktop\lumen v2\internal\mongostore\`. It contains only two files:
   - `connect.go` (lines 1–30): establishes client connection.
   - `galleries.go` (lines 1–135): defines `Galleries` struct, constructor `NewGalleries`, index creation `EnsureIndexes`, and CRUD methods (`List`, `Archive`, `Create`, `Get`, `Update`).
   No BSON models or store helpers exist for `withdrawals`, `guest_uploads`, `account_links`, or `admin_audit`.

2. **Database contracts defined in `PROJECT.md` (lines 62–67)**:
   - `accounts`: `{id: string, username: string, email: string, verified: bool, created_at: int64, password_hash: string, password_salt: string, role: string}`
   - `account_links`: `{token: string, account_id: string, purpose: string, expires_at: date}` (TTL index on expires_at)
   - `withdrawals`: `{id: string, studio_id: string, amount: int64, fee: int64, currency: string, recipient_code: string, reference: string, status: string, bank_code: string, account_number: string, account_name: string, created_at: int64, updated_at: int64}`
   - `guest_uploads`: `{id: string, studio_id: string, gallery_id: string, guest_name: string, filename: string, size: int64, status: string, file_id: ObjectID, created_at: int64}`
   - `admin_audit`: `{id: string, action: string, details: string, created_at: int64}`

3. **Existing startup index provisioning in `internal/httpapi/server.go` (lines 55–79)**:
   ```go
   _, err = db.Collection("accounts").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "username", Value: 1}}, Options: options.Index().SetUnique(true)})
   ...
   _, err = db.Collection("galleries").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)})
   ...
   _, err = db.Collection("photos").Indexes().CreateMany(ctx, []mongo.IndexModel{...})
   ...
   _, err = db.Collection("orders").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "id", Value: 1}}, Options: options.Index().SetUnique(true)})
   ...
   if r2 != nil {
       _, err = db.Collection("direct_uploads").Indexes().CreateOne(ctx, mongo.IndexModel{Keys: bson.D{{Key: "expires_at", Value: 1}}, Options: options.Index().SetExpireAfterSeconds(0)})
   }
   ```
   The startup routine is completely missing indexes for `account_links`, `withdrawals`, `guest_uploads`, `admin_audit`, and the sparse index on `accounts.email`.

4. **Existing Handlers & Consumer Expectations**:
   - `internal/httpapi/wallet.go:45-50`: Decodes `reference`, `account_last4`, `status`, `created`, `amount` from `withdrawals`.
   - `internal/httpapi/admin_activity.go:93-98`: Decodes `reference`, `bank`, `account_last4`, `amount`, `status`, `created` from `withdrawals`.
   - `internal/httpapi/insights.go:111-116`: Decodes `studio_id`, `reference`, `status`, `created`, `account_last4`, `amount` from `withdrawals`.
   - `internal/httpapi/admin_activity.go:54`: Inserts `{"id": auditID, "actor": user.Username, "action": "viewed account activity", "target": target.Username, "created": timestamp()}` into `admin_audit`.
   - `internal/httpapi/workspace.go:193`: Finds and sorts `admin_audit` by `{Key: "created", Value: -1}`.
   - `public/enhancements.js:60`: Expects `p.id`, `p.name`, `p.status`, `p.created` from `/api/galleries/:id/guest-uploads`.
   - `internal/httpapi/server.go:37-45`: The internal `account` struct is missing `Email` and `Verified` fields.

5. **Test Status**:
   - `go test ./...`: passes with exit code 0.
   - `node --test test/*.test.js`: 13 tests pass with exit code 0.

---

## 2. Logic Chain
1. *From Observation 1 & 2*: The four collections (`withdrawals`, `guest_uploads`, `account_links`, `admin_audit`) are required across Milestones M2, M3, M4, and M6. Implementing them as modular, self-contained files in `internal/mongostore/` (`withdrawals.go`, `guest_uploads.go`, `account_links.go`, `admin_audit.go`) with dedicated constructors and `EnsureIndexes` methods adheres directly to the existing pattern set by `galleries.go`.
2. *From Observation 3*: Index provisioning at startup in `httpapi.New` (`server.go`) guarantees that every database instance (including temporary test databases created during `TestMongoWorkspace`) automatically has all required unique, compound, TTL, and sparse indexes created before accepting traffic.
3. *From Observation 4*: Field naming discrepancies exist between the pure int64 epoch contract in `PROJECT.md` and the RFC3339 string timestamps used by existing code (`created` vs `created_at`). To maintain 100% backward compatibility while fully adhering to `PROJECT.md`, the BSON models must include both primary contract fields (`created_at int64`, `updated_at int64`) and compatibility fields (`created string bson:"created,omitempty"`, `account_last4 string bson:"account_last4,omitempty"`, `name string bson:"name,omitempty"`).
4. *From Observation 4*: Adding `Email string bson:"email,omitempty"` and `Verified bool bson:"verified"` to `account` in `server.go` enables session decoding and admin overview logic to function without breaking legacy accounts.
5. *From Observation 5*: Creating unit tests in `internal/mongostore/models_test.go` will test all model roundtrips and validations hermetically without requiring external database dependencies.

---

## 3. Caveats
- No live MongoDB instance is running in the current local environment unless `PICTIDE_TEST_MONGODB=1` is provided with a valid connection string. Therefore, index creation during startup will be validated via unit tests and Go build checks.
- GridFS buckets are built into mongo-driver v2 (`db.GridFSBucket()`) and already in use in `photos.go` and `direct_upload.go`. GridFS does not require manual collection creation since it provisions `fs.files` and `fs.chunks` on first upload.
- No other caveats.

---

## 4. Conclusion
The implementation strategy for Milestone M1 Schemas & Indexes is fully drafted in `c:\Users\Hp\Desktop\lumen v2\.agents\teamwork\explorer_m1_2\strategy.md`. 
The Worker agent has complete, production-ready code diffs to:
1. Create `internal/mongostore/withdrawals.go`
2. Create `internal/mongostore/guest_uploads.go`
3. Create `internal/mongostore/account_links.go`
4. Create `internal/mongostore/admin_audit.go`
5. Create `internal/mongostore/models_test.go`
6. Modify `internal/httpapi/server.go` to update `account` struct and provision all 8 required collection index specifications in `httpapi.New`.

---

## 5. Verification Method
After the Worker implements the code diff plan:
1. **Compilation Check**:
   ```powershell
   go build ./...
   ```
   *Expected outcome*: Exits with code 0.
2. **Unit Tests Execution**:
   ```powershell
   go test -v ./internal/mongostore/...
   go test -v ./internal/httpapi/...
   ```
   *Expected outcome*: All tests pass, including the new `Test*BSONRoundtrip` tests in `mongostore`.
3. **Frontend Compatibility Check**:
   ```powershell
   node --test test/*.test.js
   ```
   *Expected outcome*: 13 tests pass with 0 failures.
4. **Integration Invalidation Condition**:
   If index creation in `httpapi.New` returns an error, check if any unique constraint or TTL parameter was specified with mismatched types or invalid options.
