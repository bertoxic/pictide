package database

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"path/filepath"
)

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, statement := range []string{"PRAGMA journal_mode=WAL", "PRAGMA foreign_keys=ON", "PRAGMA busy_timeout=5000"} {
		if _, err = db.Exec(statement); err != nil {
			db.Close()
			return nil, fmt.Errorf("initialize sqlite: %w", err)
		}
	}
	if err = Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
func Migrate(db *sql.DB) error {
	schema := `CREATE TABLE IF NOT EXISTS galleries(id TEXT PRIMARY KEY,name TEXT NOT NULL,client TEXT,location TEXT,event_date TEXT,price INTEGER DEFAULT 5000,status TEXT DEFAULT 'Draft',created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS photos(id TEXT PRIMARY KEY,gallery_id TEXT NOT NULL REFERENCES galleries(id) ON DELETE CASCADE,name TEXT NOT NULL,hash TEXT NOT NULL,created TEXT NOT NULL, UNIQUE(gallery_id,hash));
CREATE TABLE IF NOT EXISTS orders(id TEXT PRIMARY KEY,gallery_id TEXT NOT NULL, email TEXT NOT NULL, amount INTEGER NOT NULL,status TEXT NOT NULL,created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS order_photos(order_id TEXT NOT NULL,photo_id TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS entitlements(token_hash TEXT PRIMARY KEY,order_id TEXT NOT NULL,downloads_used INTEGER DEFAULT 0,max_downloads INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS payment_events(reference TEXT PRIMARY KEY,provider TEXT NOT NULL,created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS withdrawals(id TEXT PRIMARY KEY,reference TEXT UNIQUE NOT NULL,amount INTEGER NOT NULL,bank TEXT NOT NULL,account_last4 TEXT NOT NULL,status TEXT NOT NULL,created TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS settings(key TEXT PRIMARY KEY,value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS imgbb_previews(photo_id TEXT NOT NULL,variant TEXT NOT NULL,signature TEXT NOT NULL,url TEXT NOT NULL,expires INTEGER NOT NULL,retry_after INTEGER NOT NULL DEFAULT 0,PRIMARY KEY(photo_id,variant));
CREATE TABLE IF NOT EXISTS guest_uploads(id TEXT PRIMARY KEY,gallery_id TEXT NOT NULL REFERENCES galleries(id) ON DELETE CASCADE,name TEXT NOT NULL,hash TEXT NOT NULL,token_hash TEXT NOT NULL,status TEXT NOT NULL,created TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS idx_photos_gallery ON photos(gallery_id);CREATE INDEX IF NOT EXISTS idx_orders_gallery ON orders(gallery_id);CREATE INDEX IF NOT EXISTS idx_order_photos_order ON order_photos(order_id);`
	if _, err := db.Exec(schema); err != nil {
		return err
	}
	columns := map[string]map[string]string{"galleries": {"archived": "INTEGER NOT NULL DEFAULT 0", "watermark_text": "TEXT NOT NULL DEFAULT 'PICTIDE · PROOF'", "watermark_style": "TEXT NOT NULL DEFAULT 'diagonal'", "watermark_logo": "TEXT NOT NULL DEFAULT ''", "watermark_opacity": "INTEGER NOT NULL DEFAULT 48", "watermark_size": "INTEGER NOT NULL DEFAULT 5", "watermark_color": "TEXT NOT NULL DEFAULT '#ffffff'", "brand_position": "TEXT NOT NULL DEFAULT 'bottom-right'", "brand_size": "INTEGER NOT NULL DEFAULT 18", "brand_opacity": "INTEGER NOT NULL DEFAULT 90", "share_title": "TEXT NOT NULL DEFAULT ''", "share_message": "TEXT NOT NULL DEFAULT ''", "share_color": "TEXT NOT NULL DEFAULT '#4f5b2a'", "share_qr_background": "TEXT NOT NULL DEFAULT '#ffffff'", "allow_guest_upload": "INTEGER NOT NULL DEFAULT 0", "card_design": "TEXT NOT NULL DEFAULT ''", "cover_photo": "TEXT NOT NULL DEFAULT ''"}, "photos": {"price": "INTEGER", "archived": "INTEGER NOT NULL DEFAULT 0"}, "order_photos": {"unit_price": "INTEGER"}, "orders": {"platform_fee": "INTEGER NOT NULL DEFAULT 0"}}
	for table, fields := range columns {
		rows, err := db.Query("PRAGMA table_info(" + table + ")")
		if err != nil {
			return err
		}
		found := map[string]bool{}
		for rows.Next() {
			var cid, notnull, pk int
			var name, typ string
			var def any
			if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
				rows.Close()
				return err
			}
			found[name] = true
		}
		if err = rows.Close(); err != nil {
			return err
		}
		for name, typ := range fields {
			if !found[name] {
				if _, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + name + " " + typ); err != nil {
					return fmt.Errorf("migrate %s.%s: %w", table, name, err)
				}
			}
		}
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_orders_created_id ON orders(created DESC,id DESC); CREATE INDEX IF NOT EXISTS idx_orders_status_created ON orders(status,created DESC,id DESC); CREATE INDEX IF NOT EXISTS idx_orders_gallery_created ON orders(gallery_id,created DESC,id DESC); CREATE INDEX IF NOT EXISTS idx_photos_gallery_created ON photos(gallery_id,archived,created DESC,id DESC); CREATE INDEX IF NOT EXISTS idx_galleries_archived_created ON galleries(archived,created DESC,id DESC); CREATE INDEX IF NOT EXISTS idx_guest_uploads_gallery_status ON guest_uploads(gallery_id,status,created DESC)`); err != nil {
		return err
	}
	return nil
}
