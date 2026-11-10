package paystack

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestInitializeAndSignature(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-secret" {
			t.Error("authorization missing")
		}
		var body map[string]any
		json.NewDecoder(r.Body).Decode(&body)
		json.NewEncoder(w).Encode(map[string]any{"status": true, "data": map[string]any{"authorization_url": "https://checkout.paystack.com/test", "reference": body["reference"]}})
	}))
	defer srv.Close()
	c := New("test-secret")
	c.BaseURL = srv.URL
	if _, err := c.Initialize(context.Background(), "test@example.com", 120000, "ref", "https://example.com"); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"event":"charge.success"}`)
	mac := hmac.New(sha512.New, []byte("test-secret"))
	mac.Write(body)
	if !c.VerifySignature(body, hex.EncodeToString(mac.Sum(nil))) || c.VerifySignature(body, "bad") {
		t.Fatal("signature validation failed")
	}
}
