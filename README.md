# Pictide Studio

Pictide is being rebuilt on MongoDB. The current implementation supports account sign-in and creation, studio settings, gallery creation and editing, photo uploads and proof previews, cover selection, gallery customization, QR codes, account password changes, and account suspension. Photos and their generated derivatives are stored in MongoDB GridFS, including on Vercel.

Checkout, payment verification, and paid downloads have MongoDB implementations but have not been tested in this conversion. Payouts, guest submissions, account recovery, registration by email, print layouts, brand image upload and regeneration, and some administration and reporting functions are not implemented. Unsupported routes return `503`. Do not treat this demo as production-ready.

## Run locally

Install Go 1.23.3 or later and provide a reachable MongoDB instance. Copy `.env.example` to `.env` and set `MONGODB_URI`, `PAYSTACK_SECRET_KEY`, `LUMEN_ADMIN_PASSWORD` (at least 12 characters), and `LUMEN_SIGNING_KEY` (at least 32 characters). `MONGODB_DATABASE` defaults to `pictide`. The initial administrator is `admin`; the password is used only when the account is first created. Then run:

```powershell
go run -p 1 ./cmd/pictide
```

The server listens on `127.0.0.1:3000` by default. `LUMEN_HOST` and `PORT` override the address. Configure `PUBLIC_URL` as an HTTPS origin when the server is publicly reachable. Credentials in `.env` should remain private.

For Vercel, select the Go preset and repository root (`./`). The root `main.go` is the detected entrypoint. Set `MONGODB_URI`, `PAYSTACK_SECRET_KEY`, `LUMEN_ADMIN_PASSWORD`, `LUMEN_SIGNING_KEY`, `PUBLIC_URL` (the HTTPS deployment origin), and `LUMEN_HOST=0.0.0.0` in the project environment. `MONGODB_DATABASE` is optional. Vercel must be able to reach MongoDB.

Vercel limits function request and response bodies to 4.5 MB. The current direct photo upload and download routes do not support chunking or direct-to-object-storage transfers, so larger uploads, downloads, and ZIP archives can fail on Vercel. Host the server elsewhere or use smaller files until those routes are replaced.

For an opt-in workspace test, set `PICTIDE_TEST_MONGODB=1` and run `go test -run '^TestMongoWorkspace$' ./internal/httpapi`. It uses a randomly named temporary MongoDB database and does not test payment or studio isolation.
