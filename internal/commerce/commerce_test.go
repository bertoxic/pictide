package commerce

import (
	"context"
	"database/sql"
	_ "modernc.org/sqlite"
	"sync"
	"testing"
)

func TestCompletionAndAtomicQuota(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`CREATE TABLE orders(id TEXT PRIMARY KEY,status TEXT);CREATE TABLE payment_events(reference TEXT PRIMARY KEY,provider TEXT,created TEXT);CREATE TABLE order_photos(order_id TEXT,photo_id TEXT);CREATE TABLE entitlements(token_hash TEXT PRIMARY KEY,order_id TEXT,downloads_used INTEGER,max_downloads INTEGER);INSERT INTO orders VALUES('order','Pending');INSERT INTO order_photos VALUES('order','photo');INSERT INTO entitlements VALUES('hash','order',0,3);`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err = ConsumeDownload(ctx, db, "hash", "photo"); err != ErrForbidden {
		t.Fatal("pending order unlocked")
	}
	if err = CompletePayment(ctx, db, "order", "ref", "demo"); err != nil {
		t.Fatal(err)
	}
	if err = CompletePayment(ctx, db, "order", "ref", "demo"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	results := make(chan error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); results <- ConsumeDownload(ctx, db, "hash", "photo") }()
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else if e != ErrForbidden {
			t.Fatal(e)
		}
	}
	if success != 3 {
		t.Fatalf("got %d downloads, want 3", success)
	}
}
