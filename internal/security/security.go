package security

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/scrypt"
)

func Token() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
func HashToken(token string) string {
	h := sha256.Sum256([]byte(token))
	return hex.EncodeToString(h[:])
}

// PasswordHash uses the same scrypt settings and storage format as the existing server.
func PasswordHash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	saltHex := hex.EncodeToString(salt)
	key, err := scrypt.Key([]byte(password), []byte(saltHex), 16384, 8, 1, 64)
	if err != nil {
		return "", err
	}
	return saltHex + ":" + hex.EncodeToString(key), nil
}
func CheckPassword(stored, password string) bool {
	parts := strings.Split(stored, ":")
	if len(parts) != 2 {
		return false
	}
	key, err := scrypt.Key([]byte(password), []byte(parts[0]), 16384, 8, 1, 64)
	if err != nil {
		return false
	}
	expected, err := hex.DecodeString(parts[1])
	return err == nil && hmac.Equal(key, expected)
}
func SignDownload(key, photo string, expires int64) string {
	m := hmac.New(sha256.New, []byte(key))
	fmt.Fprintf(m, "%s:%d", photo, expires)
	return hex.EncodeToString(m.Sum(nil))
}
func CheckDownload(key, photo, signature string, expires int64, now time.Time) bool {
	if expires < now.Unix() || expires > now.Unix()+90 {
		return false
	}
	a, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	b, _ := hex.DecodeString(SignDownload(key, photo, expires))
	return hmac.Equal(a, b)
}
func SessionSignature(key string, expires int64, version string) string {
	m := hmac.New(sha256.New, []byte(key))
	m.Write([]byte(strconv.FormatInt(expires, 10) + "." + version))
	return hex.EncodeToString(m.Sum(nil))
}
