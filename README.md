# Pictide Studio

Pictide is being converted to MongoDB. The former application handlers have been removed and **the gallery, account, upload, checkout, payment, and other application features are unavailable**. The browser assets and independent media, payment-provider, security, and QR helper packages remain in the repository; they are not wired into a functional application.

The current Go server connects to MongoDB, creates the gallery index, and responds to every HTTP request with `503 Service Unavailable`. It is a conversion foundation, not a working studio application.

## Run the conversion service

Install Go 1.23.3 or later and provide a reachable MongoDB instance. Copy `.env.example` to `.env`, set `MONGODB_URI` and optionally `MONGODB_DATABASE` (default `pictide`), and configure `PAYSTACK_SECRET_KEY` as required by the current configuration loader. Then run:

```powershell
go run -p 1 ./cmd/pictide
```

The server listens on `127.0.0.1:3000` by default. `LUMEN_HOST` and `PORT` override the address. MongoDB connection and index errors prevent startup. Credentials in `.env` should remain private.

The root `main.go` is also the entrypoint for Vercel's Go backend detection. Configure `MONGODB_URI` and `PAYSTACK_SECRET_KEY` in the Vercel project environment; `MONGODB_DATABASE` is optional. A successful build does not make the removed application features available.

The MongoDB gallery helper scopes its operations by studio ID. Application features and the remaining data models still need implementation before this project can be used as a studio.
