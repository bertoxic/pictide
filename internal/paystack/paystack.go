package paystack

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	Secret, BaseURL string
	HTTP            *http.Client
}

func New(secret string) *Client {
	return &Client{Secret: secret, BaseURL: "https://api.paystack.co", HTTP: &http.Client{Timeout: 15 * time.Second}}
}
func (c *Client) Call(ctx context.Context, method, endpoint string, input, output any) error {
	if c.Secret == "" {
		return errors.New("Paystack is not configured")
	}
	var body io.Reader
	if input != nil {
		b, err := json.Marshal(input)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+endpoint, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.Secret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("Paystack unavailable: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Status  bool            `json:"status"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err = json.NewDecoder(io.LimitReader(resp.Body, 2<<20)).Decode(&result); err != nil {
		return errors.New("invalid Paystack response")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || !result.Status {
		return fmt.Errorf("Paystack: %s", result.Message)
	}
	if output != nil {
		return json.Unmarshal(result.Data, output)
	}
	return nil
}
func (c *Client) Initialize(ctx context.Context, email string, amount int64, reference, callback string) (string, error) {
	var out struct {
		URL       string `json:"authorization_url"`
		Reference string `json:"reference"`
	}
	err := c.Call(ctx, "POST", "/transaction/initialize", map[string]any{"email": email, "amount": amount, "currency": "NGN", "reference": reference, "callback_url": callback}, &out)
	if err != nil {
		return "", err
	}
	u, e := url.Parse(out.URL)
	if e != nil || u.Scheme != "https" || u.Hostname() != "checkout.paystack.com" || out.Reference != reference {
		return "", errors.New("invalid checkout response")
	}
	return out.URL, nil
}

type Transaction struct {
	Status, Reference, Currency string
	Amount                      int64
}

func (c *Client) Verify(ctx context.Context, reference string) (Transaction, error) {
	var t Transaction
	err := c.Call(ctx, "GET", "/transaction/verify/"+url.PathEscape(reference), nil, &t)
	return t, err
}
func (c *Client) VerifySignature(body []byte, signature string) bool {
	if c.Secret == "" {
		return false
	}
	provided, err := hex.DecodeString(signature)
	if err != nil {
		return false
	}
	mac := hmac.New(sha512.New, []byte(c.Secret))
	mac.Write(body)
	return hmac.Equal(provided, mac.Sum(nil))
}
