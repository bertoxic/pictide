# Pictide Studio

Pictide is a Go-powered photo proofing and delivery platform with separate photographer studios and a platform administrator. It supports galleries, individual and free photo prices, protected downloads, guest contributions with photographer approval, watermark and brand-mark controls, share cards, QR codes, Paystack checkout, and bank payouts. The browser interface is written in HTML, CSS and JavaScript; Node.js is not required to run the server.

Pictide was previously named Lumen. Existing `LUMEN_` environment variables, database paths, cookies, and browser storage keys remain in use for compatibility with existing installations and purchases. Existing gallery watermark settings and generated photos are preserved; change a gallery's watermark and regenerate its photos if you want to update existing proofs.

## Run locally

Requires Go 1.23.3 or newer. In PowerShell, from this folder:

```powershell
go run -p 1 ./cmd/pictide
```

Alternatively run `./start.ps1` from the project folder. Open http://127.0.0.1:3000. The server loads `.env` automatically. A studio password and signing key are required at startup. If they were generated during migration, find `LUMEN_ADMIN_PASSWORD` in your local `.env`; you can change the password from Settings. Keep `.env` private. `LUMEN_ENV_FILE` can point to a different environment file, and existing process variables take precedence.

If your terminal is in the project's `cmd` folder, `go run ./pictide` also uses the project root for `.env`, data, and public files. Relative paths in `LUMEN_ENV_FILE` and other settings are resolved from that root in this case.

The existing SQLite database and media paths are preserved as the administrator’s original studio. `LUMEN_DATA_DIR` sets the data directory. `LUMEN_DB_PATH` or legacy `DB_PATH` can select a database, and `LUMEN_MEDIA_DIR` can select a media directory. The old `STORAGE_DIR` key is not used, to avoid silently switching away from existing media. Back up the platform account database, every studio database (including SQLite WAL state), and all studio media together.

## Share and print

Open a gallery, choose **Share**, then **Design card**. The studio saves a separate design for each gallery. Choose a format and layout, add text, shapes, and PNG/JPEG/WebP logos, drag or nudge layers, reorder them, and adjust colors, opacity, rotation, and typography. Guides are only shown in the editor. Download a high-resolution PNG or use the browser print dialog to save a PDF. The share dialog also controls the guest title, welcome message, and QR foreground/background colors. If an HTTPS PUBLIC_URL is set, share links use it.

Saved designs have a 1.2 MB size limit; use smaller images if a design exceeds it. QR warnings flag overlap and low-contrast or inverted colors, but always scan the final exported or printed code before sharing.


## Photographer accounts and administration

On first startup after upgrading, the existing studio is retained as the `admin` account. Sign in with username `admin` and the studio password you used before the upgrade (or `LUMEN_ADMIN_PASSWORD` if no password was saved in Settings). The administrator can create photographer accounts under **Photographers**; each photographer signs in with their own username and password and can change the password in Settings. Photographers can self-register after email delivery is configured; verification is required before their first sign-in. Existing accounts should verify a recovery email in Settings, and the administrator can do so from the admin console.

The platform stores account credentials in `platform.db` beside the configured studio database. New studios have separate SQLite databases and media under `studios/<account-id>/` beside that database. Back up and restore the platform database, original studio database, all studio directories, and the persistent signing key together. Restart the server after upgrading. Existing sessions require signing in again.

The platform collects guest payments through its configured Paystack account. Each studio has its own order and withdrawal records and calculated balance; a photographer can request a payout from their own wallet. Provider settlement, fees, refunds and chargebacks still require reconciliation, and Paystack transfers may require dashboard approval or OTP.



## ImgBB preview storage

To use ImgBB for public-facing previews, set `IMGBB_API_KEY` and `LUMEN_IMGBB_PREVIEWS=true`, then restart Pictide. Only proof JPEGs and proof thumbnails from published, non-archived galleries are uploaded, on demand when viewed. The original files, clean previews, paid downloads, draft photographs and pending guest submissions remain local. Existing published previews are uploaded as they are accessed; this is not a bulk migration or a backup of the originals.

Pictide uses its configured image storage as the authoritative copy and falls back to it if ImgBB is unavailable. It checks the retrieved preview matches the local file before serving it. ImgBB copies are requested with a 24-hour expiration and are refreshed on later views. ImgBB provides direct image links: anyone who obtains a link can access that preview, potentially until it expires even if the gallery is subsequently unpublished or the photo removed. Proofs may be unwatermarked if watermarking was disabled. Review [ImgBB's terms](https://imgbb.com/tos) before using it for clients’ photographs; its terms grant ImgBB broad rights over uploaded content. Do not use this integration as private cloud storage for paid originals.

Set `LUMEN_IMGBB_PREVIEWS=false` and restart to stop new ImgBB preview requests. Already uploaded copies may remain accessible until their requested expiry. For an optional synthetic live test, set `LUMEN_TEST_IMGBB=1` and `LUMEN_ENV_FILE` to the absolute path of your environment file, then run `go test -count=1 -run '^TestLiveImgBBPublishedPreview$' ./internal/httpapi`. The test uploads only a generated JPEG with a 60-second expiry.

## Registration and account recovery

Set `PUBLIC_URL` to the public HTTPS origin and configure `LUMEN_SMTP_HOST`, `LUMEN_SMTP_PORT` (default 587), `LUMEN_SMTP_USER`, `LUMEN_SMTP_PASSWORD` and `LUMEN_SMTP_FROM`. The SMTP server must support STARTTLS. Restart the server after changing these settings. Without email delivery, self-registration and email-based password recovery are unavailable. New accounts must verify their email before signing in. Existing accounts can verify an address in Settings; the administrator can do so in the administration console. Links expire after 30 minutes, work once, and password resets sign out existing sessions. Keep SMTP credentials private and test delivery before enabling public registration.

The administrator signs in to a separate platform console with platform sales trends, finance and workload summaries, account management, recent orders and withdrawals, suspension/restoration, status refresh, and an access history. Sales trends use UTC order creation dates and current verified payment status. Order and withdrawal lists show the most recent 200 records; individual studio activity is limited to 100 of each. Administrative status refresh checks Paystack and records the action. Suspension stops account sign-in and revokes sessions; existing published galleries and orders remain accessible. The original migrated studio can be opened from the console. The console does not currently perform refunds, chargeback handling, payout approval, account deletion, or provider reconciliation. It does not replace the Paystack dashboard.

## Paystack and deployment

Go checkout uses Paystack only; there is no demo payment mode. Set `LUMEN_PAYMENT_MODE=paystack` and `PAYSTACK_SECRET_KEY` in `.env`. Paystack test keys use Paystack's test environment; live keys can initiate real checkout. The server does not make a transaction at startup. Use the Paystack test environment to validate the full flow before using a live key.

For public hosting, configure a secure reverse proxy with HTTPS, an appropriate persistent data volume, and these settings:

- `LUMEN_HOST`: the appropriate interface for your deployment.
- `PUBLIC_URL`: the public HTTPS origin, for example `https://photos.example.com`.
- `LUMEN_ADMIN_PASSWORD`: a long unique bootstrap studio password.
- `LUMEN_SIGNING_KEY`: a persistent random secret of at least 32 characters. Changing it invalidates sessions and outstanding signed download links.
- `PAYSTACK_SECRET_KEY`: the intended Paystack secret key.
- `LUMEN_PLATFORM_FEE_BPS`: optional fee from 0 to 10000 basis points, captured per order; provider fees are separate.
- `NODE_ENV=production`: enables the strict production configuration checks and secure session cookies.

Configure Paystack's webhook to `https://your-domain/api/v1/webhooks/paystack`. The server validates signed webhooks and checks successful payment references, amounts, and currency before unlocking photos. Transfers may require Paystack dashboard approval or OTP. If the outcome of a transfer is unknown, its amount stays reserved until reconciled; check Paystack before retrying. The wallet checks the Paystack balance before initiating a payout, but provider fees, refunds, and settlement timing are not fully reconciled automatically.

## Tests and migration

Run `go test -p 1 ./...`. The tests exercise the Go server's gallery, upload, moderation, payment-verification, webhook, signed-download and quota flows using a local Paystack stub. They do not perform a live Paystack charge or transfer. The prior Node implementation is retained in `legacy/` for reference only and is not used by the app.

## Performance checks

Photographer gallery and order lists use server-side pages. Order totals and filters apply across all matching orders rather than only the current page. Photo grids render in batches and defer image loading; the gallery API still loads all photo metadata. Public resource routing uses a bounded in-process cache, with a studio scan on cold or stale lookups. Admin lists remain limited rather than fully paginated. Database indexes support common list and status filters.

A reproducible local benchmark creates a temporary database with 10,000 synthetic orders and exercises the paged order endpoint, including its aggregate queries:

```powershell
go test -run '^$' -bench '^BenchmarkPagedOrders' -benchtime=2s ./internal/httpapi
go test -run '^$' -bench '^BenchmarkStudioRouting' -benchtime=2s ./internal/platform
```

These are isolated microbenchmarks, not production load tests. Run broader concurrency, upload, checkout, and multi-studio load tests on a staging environment with representative data before setting capacity targets. The current SQLite-per-studio design, single connection per studio, first-time cross-studio scans, in-memory caches, unpaged photo metadata and admin lists, and offset pagination remain limits for high-scale or multiple-instance deployments.

## Remaining production work

Guest purchase and upload recovery currently depend on the original browser; purchase delivery, cross-device purchase recovery, two-factor authentication, email notifications, and MongoDB synchronization are not implemented. Paystack integration must be tested with your account and reachable HTTPS webhook. Independent security review, backup and restore drills, operational monitoring, and full provider settlement/refund reconciliation are still required before treating this as production-ready. Free space on the system drive before building or uploading large photos; if necessary, set Go's `GOCACHE` and `GOTMPDIR` to directories on a drive with sufficient space.
# pictide

## Cloudflare R2 image storage

Set `R2_ACCOUNT_ID`, `R2_ACCESS_KEY_ID`, `R2_SECRET_ACCESS_KEY`, and `R2_BUCKET_NAME` together to store newly uploaded photos, generated image variants, guest submissions, and logos in a private R2 bucket. Restart the server after changing the environment. Each studio uses a separate object prefix. When R2 is configured, the app migrates existing media at startup, verifies each uploaded object against the local file, and removes the local copy only after verification. New media is stored in R2 without a persistent local copy. Startup stops if migration fails or a remote object differs from its local counterpart. Back up the databases separately as well. R2 credentials must allow object reads, writes, and deletes. ImgBB preview caching, when enabled, remains separate and only receives public-facing previews.
