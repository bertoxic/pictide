package imgbb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const MaxImageBytes = 32 << 20
const DefaultExpiry = 24 * time.Hour

type Client struct {
	key         string
	endpoint    string
	http        *http.Client
	allowedHost func(*url.URL) bool
	Expiration  time.Duration
}

func validImageURL(u *url.URL) bool {
	return u != nil && u.Scheme == "https" && u.User == nil && u.Port() == "" && u.Hostname() == "i.ibb.co"
}
func New(key string) *Client {
	c := &Client{key: key, endpoint: "https://api.imgbb.com/1/upload", allowedHost: validImageURL, Expiration: DefaultExpiry}
	c.http = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 4 || !c.allowedHost(req.URL) {
			return errors.New("image redirect not allowed")
		}
		return nil
	}}
	return c
}
func (c *Client) Upload(ctx context.Context, data []byte) (string, error) {
	if c.key == "" {
		return "", errors.New("ImgBB is not configured")
	}
	if len(data) == 0 || len(data) > MaxImageBytes {
		return "", errors.New("image exceeds ImgBB size limit")
	}
	if c.Expiration < time.Minute || c.Expiration > 180*24*time.Hour {
		return "", errors.New("invalid ImgBB expiry")
	}
	values := url.Values{"key": {c.key}, "image": {base64.StdEncoding.EncodeToString(data)}, "expiration": {fmt.Sprint(int64(c.Expiration.Seconds()))}}
	req, e := http.NewRequestWithContext(ctx, "POST", c.endpoint, strings.NewReader(values.Encode()))
	if e != nil {
		return "", e
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, e := c.http.Do(req)
	if e != nil {
		return "", errors.New("ImgBB upload unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("ImgBB upload returned HTTP %d", resp.StatusCode)
	}
	var out struct {
		Success bool `json:"success"`
		Data    struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); e != nil || !out.Success {
		return "", errors.New("invalid ImgBB upload response")
	}
	u, e := url.Parse(out.Data.URL)
	if e != nil || !c.allowedHost(u) {
		return "", errors.New("invalid ImgBB image URL")
	}
	return u.String(), nil
}
func (c *Client) Download(ctx context.Context, raw string) ([]byte, error) {
	u, e := url.Parse(raw)
	if e != nil || !c.allowedHost(u) {
		return nil, errors.New("invalid ImgBB image URL")
	}
	req, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return nil, e
	}
	resp, e := c.http.Do(req)
	if e != nil {
		return nil, errors.New("ImgBB image unavailable")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ImgBB image returned HTTP %d", resp.StatusCode)
	}
	data, e := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes+1))
	if e != nil || len(data) == 0 || len(data) > MaxImageBytes || !bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}) {
		return nil, errors.New("invalid ImgBB image")
	}
	return data, nil
}
