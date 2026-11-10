package security

import (
	"testing"
	"time"
)

func TestPasswordAndSignedDownloads(t *testing.T) {
	hash, err := PasswordHash("correct horse battery")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "correct horse battery") || CheckPassword(hash, "incorrect") {
		t.Fatal("password verification failed")
	}
	now := time.Unix(1000, 0)
	sig := SignDownload("secret", "photo", 1090)
	if !CheckDownload("secret", "photo", sig, 1090, now) || CheckDownload("secret", "other", sig, 1090, now) || CheckDownload("secret", "photo", sig, 1090, now.Add(91*time.Second)) {
		t.Fatal("download signature verification failed")
	}
}
