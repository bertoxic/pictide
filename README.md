# Pictide Studio

Pictide is being rebuilt on MongoDB. The current implementation supports account sign-in and creation, studio settings, gallery creation and editing, photo uploads and proof previews, cover selection, gallery customization, QR codes, account password changes, and account suspension. Photo metadata and previews are stored in MongoDB; when R2 is configured, originals and full-resolution downloads are stored in the private R2 bucket.

Checkout, payment verification, and paid downloads have MongoDB implementations but have not been tested in this conversion. Payouts, guest submissions, account recovery, registration by email, print layouts, brand image upload and regeneration, and some administration and reporting functions are not implemented. Unsupported routes return `503`. Do not treat this demo as production-ready.

## Run locally

Install Go 1.23.3 or later and provide a reachable MongoDB instance. Copy `.env.example` to `.env` and set `MONGODB_URI`, `PAYSTACK_SECRET_KEY`, `LUMEN_ADMIN_PASSWORD` (at least 12 characters), and `LUMEN_SIGNING_KEY` (at least 32 characters). `MONGODB_DATABASE` defaults to `pictide`. The initial administrator is `admin`; the password is used only when the account is first created. Then run:

```powershell
go run -p 1 ./cmd/pictide
```

The server listens on `127.0.0.1:3000` by default. `LUMEN_HOST` and `PORT` override the address. Configure `PUBLIC_URL` as an HTTPS origin when the server is publicly reachable. Credentials in `.env` should remain private.

For Vercel, select the Go preset and repository root (`./`). The root `main.go` is the detected entrypoint. Set `MONGODB_URI`, `PAYSTACK_SECRET_KEY`, `LUMEN_ADMIN_PASSWORD`, `LUMEN_SIGNING_KEY`, `PUBLIC_URL` (the HTTPS deployment origin), and `LUMEN_HOST=0.0.0.0` in the project environment. Also set `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, and `R2_BUCKET_NAME` there: the local `.env` file is not deployed. `MONGODB_DATABASE` is optional. Vercel must be able to reach MongoDB and R2.

With R2 configured, the browser uploads each original directly to R2 with a short-lived signed URL, avoiding Vercel's 4.5 MB request limit. The server retrieves and processes the image, and directs authorized full-resolution downloads and free-photo ZIP downloads to temporary signed R2 URLs. Existing GridFS master images are copied to R2 when downloaded. Each uploaded original is limited to 30 MiB; image processing and ZIP creation still consume function time and memory. If a browser cannot reach R2, images of up to 4,000,000 bytes fall back to the server and GridFS; larger images require the R2 connection. Without R2, uploads pass through the function and remain subject to Vercel's body-size limit.

Browser uploads require a CORS rule on the R2 bucket. Add the exact Vercel app origin to an allowed rule for method `PUT` and header `Content-Type`. For local testing, also allow the origin used to access the app (for example, `http://127.0.0.1:8080`). Preserve any other rules the bucket already uses. Cloudflare's [R2 CORS documentation](https://developers.cloudflare.com/r2/buckets/cors/) describes the dashboard configuration. Signed upload URLs remain usable for up to 10 minutes and signed download URLs for up to two minutes; anyone holding one can use it until expiry.

For a bucket with no existing CORS rules, an example policy is below. Replace the placeholder with the actual HTTPS origin; add any separate preview or custom-domain origins that need upload access.

```json
[
  {
    "AllowedOrigins": ["https://YOUR-APP.vercel.app", "http://127.0.0.1:8080"],
    "AllowedMethods": ["PUT"],
    "AllowedHeaders": ["Content-Type"],
    "MaxAgeSeconds": 3600
  }
]
```

Configure an R2 lifecycle rule to expire objects under `pictide/temporary/` after a day. Successful source uploads are deleted after processing, but interrupted uploads and generated ZIP archives can remain there until lifecycle cleanup. MongoDB's upload-job TTL does not delete R2 objects.

For an opt-in workspace test, set `PICTIDE_TEST_MONGODB=1` and run `go test -run '^TestMongoWorkspace$' ./internal/httpapi`. It uses a randomly named temporary MongoDB database and does not test payment or studio isolation.

To verify R2 with a synthetic image larger than 4.5 MB, set `PICTIDE_TEST_R2=1` and run `go test -timeout 4m -v -run '^TestR2DirectUpload$' ./internal/httpapi`. The test uses a temporary database and deletes only its own randomly named R2 objects; it does not test Paystack or studio isolation.
