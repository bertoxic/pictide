package commerce

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

var ErrForbidden = errors.New("photo is not available or download limit reached")

// CompletePayment records a reference and updates the order in one transaction.
// Repeated notifications never replace entitlement hashes or credit the order twice.
func CompletePayment(ctx context.Context, db *sql.DB, order, reference, provider string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	err = tx.QueryRowContext(ctx, "SELECT 1 FROM payment_events WHERE reference=?", reference).Scan(&exists)
	if err == nil {
		return tx.Commit()
	}
	if err != sql.ErrNoRows {
		return err
	}
	result, err := tx.ExecContext(ctx, "UPDATE orders SET status='Paid' WHERE id=? AND status IN ('Pending','Failed')", order)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if changed == 0 {
		return errors.New("order is not pending")
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO payment_events(reference,provider,created) VALUES(?,?,?)", reference, provider, time.Now().UTC().Format(time.RFC3339Nano))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ConsumeDownload atomically increments the quota only when the paid order owns the photo.
func ConsumeDownload(ctx context.Context, db *sql.DB, tokenHash, photoID string) error {
	result, err := db.ExecContext(ctx, `UPDATE entitlements SET downloads_used=downloads_used+1 WHERE token_hash=? AND downloads_used<max_downloads AND EXISTS (SELECT 1 FROM orders o JOIN order_photos p ON p.order_id=o.id WHERE o.id=entitlements.order_id AND o.status='Paid' AND p.photo_id=?)`, tokenHash, photoID)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return ErrForbidden
	}
	return nil
}
