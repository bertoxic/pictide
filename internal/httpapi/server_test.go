package httpapi

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"pictide.local/studio/internal/config"
	"pictide.local/studio/internal/database"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoPlatformEndToEnd(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "lumen.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	c := config.Config{MediaDir: filepath.Join(dir, "media"), AdminPassword: "test-password", SigningKey: "test-signing-key", PaystackSecret: "test-secret", PublicURL: "https://photos.example.test"}
	app, err := New(db, c)
	if err != nil {
		t.Fatal(err)
	}
	var ref string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/transaction/initialize":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			ref = b["reference"].(string)
			json.NewEncoder(w).Encode(map[string]any{"status": true, "data": map[string]any{"reference": ref, "authorization_url": "https://checkout.paystack.com/test"}})
		default:
			json.NewEncoder(w).Encode(map[string]any{"status": true, "data": map[string]any{"status": "success", "reference": ref, "currency": "NGN", "amount": 300000}})
		}
	}))
	defer provider.Close()
	app.Paystack.BaseURL = provider.URL
	server := httptest.NewServer(app.Router())
	defer server.Close()
	var cookie *http.Cookie
	request := func(method, path string, body io.Reader, kind string, auth bool) *http.Response {
		t.Helper()
		req, e := http.NewRequest(method, server.URL+path, body)
		if e != nil {
			t.Fatal(e)
		}
		if kind != "" {
			req.Header.Set("Content-Type", kind)
		}
		if auth && cookie != nil {
			req.AddCookie(cookie)
		}
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	jsonReq := func(method, path string, v any, auth bool) *http.Response {
		b, _ := json.Marshal(v)
		return request(method, path, bytes.NewReader(b), "application/json", auth)
	}
	asMap := func(res *http.Response) map[string]any {
		t.Helper()
		defer res.Body.Close()
		var m map[string]any
		if e := json.NewDecoder(res.Body).Decode(&m); e != nil {
			t.Fatalf("decode %s: %v", res.Status, e)
		}
		return m
	}
	check := func(res *http.Response, status int) {
		t.Helper()
		if res.StatusCode != status {
			body, _ := io.ReadAll(res.Body)
			res.Body.Close()
			t.Fatalf("got %d want %d: %s", res.StatusCode, status, body)
		}
		res.Body.Close()
	}
	cfg := asMap(request("GET", "/api/config", nil, "", false))
	if cfg["public_url"] != "https://photos.example.test" {
		t.Fatal("public share origin missing")
	}
	check(request("GET", "/api/galleries", nil, "", false), 401)
	login := jsonReq("POST", "/api/login", map[string]any{"password": "test-password"}, false)
	if login.StatusCode != 200 {
		t.Fatal("login failed", login.Status)
	}
	cookie = login.Cookies()[0]
	login.Body.Close()
	gallery := asMap(jsonReq("POST", "/api/galleries", map[string]any{"name": "Test gallery", "price": 3000}, true))
	id := gallery["id"].(string)
	imageBytes := func(v uint8) []byte {
		img := image.NewNRGBA(image.Rect(0, 0, 80, 60))
		for y := 0; y < 60; y++ {
			for x := 0; x < 80; x++ {
				img.Set(x, y, color.NRGBA{R: v, G: 70, B: 90, A: 255})
			}
		}
		var out bytes.Buffer
		png.Encode(&out, img)
		return out.Bytes()
	}
	upload := func(path string, v uint8, auth bool) *http.Response {
		var body bytes.Buffer
		form := multipart.NewWriter(&body)
		part, _ := form.CreateFormFile("photos", "photo.png")
		part.Write(imageBytes(v))
		form.Close()
		return request("POST", path, &body, form.FormDataContentType(), auth)
	}
	up := asMap(upload("/api/galleries/"+id+"/photos", 120, true))
	if up["added"].(float64) != 1 {
		t.Fatal("photo upload failed", up)
	}
	dup := asMap(upload("/api/galleries/"+id+"/photos", 120, true))
	if dup["added"].(float64) != 0 {
		t.Fatal("duplicate accepted")
	}
	check(request("GET", "/api/galleries/"+id, nil, "", false), 404)
	check(jsonReq("PATCH", "/api/galleries/"+id, map[string]any{"status": "Published"}, true), 200)
	detail := asMap(request("GET", "/api/galleries/"+id, nil, "", false))
	photo := detail["photos"].([]any)[0].(map[string]any)["id"].(string)
	check(jsonReq("PATCH", "/api/galleries/"+id+"/customization", map[string]any{"share_color": "#123456", "share_qr_background": "#eeeedd"}, true), 200)
	qrResponse := request("GET", "/api/galleries/"+id+"/qr?format=png", nil, "", false)
	qrImage, decodeErr := png.Decode(qrResponse.Body)
	qrResponse.Body.Close()
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	rr, gg, bb, _ := qrImage.At(0, 0).RGBA()
	if rr>>8 != 0xee || gg>>8 != 0xee || bb>>8 != 0xdd {
		t.Fatal("saved QR background not applied")
	}
	svgResponse := request("GET", "/api/galleries/"+id+"/qr", nil, "", false)
	svgBody, _ := io.ReadAll(svgResponse.Body)
	svgResponse.Body.Close()
	if !strings.Contains(string(svgBody), "#eeeedd") || !strings.Contains(string(svgBody), "#123456") {
		t.Fatal("saved QR colors not present in SVG")
	}

	check(request("GET", "/api/photos/"+photo+"/clean", nil, "", false), 403)
	check(request("GET", "/api/photos/"+photo+"/clean?size=thumb", nil, "", false), 403)
	order := asMap(jsonReq("POST", "/api/checkout", map[string]any{"galleryId": id, "email": "guest@example.com", "photos": []string{photo}, "expectedAmount": 300000}, false))
	if order["amount"].(float64) != 300000 || order["mode"] != "paystack" {
		t.Fatal("bad order", order)
	}
	token := order["token"].(string)
	check(jsonReq("POST", "/api/photos/"+photo+"/download-link", map[string]any{"token": token}, false), 403)
	event, _ := json.Marshal(map[string]any{"event": "charge.success", "data": map[string]any{"reference": ref, "status": "success", "currency": "NGN", "amount": 300000}})
	mac := hmac.New(sha512.New, []byte("test-secret"))
	mac.Write(event)
	signature := hex.EncodeToString(mac.Sum(nil))
	hook := func(sig string) *http.Response {
		req, _ := http.NewRequest("POST", server.URL+"/api/v1/webhooks/paystack", bytes.NewReader(event))
		req.Header.Set("X-Paystack-Signature", sig)
		res, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		return res
	}
	check(hook("invalid"), 401)
	check(hook(signature), 200)
	check(hook(signature), 200)
	verified := asMap(jsonReq("POST", "/api/checkout/"+ref+"/verify", map[string]any{"token": token}, false))
	if verified["status"] != "Paid" {
		t.Fatal("not paid", verified)
	}
	for i := 0; i < 3; i++ {
		link := asMap(jsonReq("POST", "/api/photos/"+photo+"/download-link", map[string]any{"token": token}, false))
		check(request("GET", link["url"].(string), nil, "", false), 200)
	}
	check(jsonReq("POST", "/api/photos/"+photo+"/download-link", map[string]any{"token": token}, false), 403)
	check(request("GET", "/api/galleries/"+id+"/qr?format=png", nil, "", false), 200)
	printRes := request("GET", "/api/galleries/"+id+"/print?theme=luxury", nil, "", true)
	printBody, _ := io.ReadAll(printRes.Body)
	printRes.Body.Close()
	if printRes.StatusCode != 200 || strings.Contains(string(printBody), "ZgotmplZ") || !strings.Contains(string(printBody), "#24251f") {
		t.Fatalf("print card invalid: %s", printBody)
	}
	check(upload("/api/galleries/"+id+"/guest-uploads", 180, false), 403)
	check(jsonReq("PATCH", "/api/galleries/"+id+"/customization", map[string]any{"allow_guest_upload": 1}, true), 200)
	submitted := asMap(upload("/api/galleries/"+id+"/guest-uploads", 180, false))
	pending := submitted["added"].([]any)[0].(map[string]any)["id"].(string)
	check(request("GET", "/media/"+pending, nil, "", false), 404)
	check(jsonReq("POST", "/api/guest-uploads/"+pending+"/review", map[string]any{"action": "approve"}, true), 200)
	check(request("GET", "/api/photos/"+pending+"/free", nil, "", false), 200)
	check(request("GET", "/api/photos/"+pending+"/clean?size=thumb", nil, "", false), 200)
	free := asMap(jsonReq("POST", "/api/checkout", map[string]any{"galleryId": id, "email": "guest@example.com", "photos": []string{pending}, "expectedAmount": 0}, false))
	if free["mode"] != "free" {
		t.Fatal("free checkout failed", free)
	}
	mixed := asMap(jsonReq("POST", "/api/checkout", map[string]any{"galleryId": id, "email": "guest@example.com", "photos": []string{pending, photo}, "expectedAmount": 300000}, false))
	if mixed["amount"] != float64(300000) {
		t.Fatal("mixed checkout total incorrect", mixed)
	}
	check(jsonReq("POST", "/api/orders/"+ref+"/sync", nil, false), 401)
	synced := asMap(jsonReq("POST", "/api/orders/"+ref+"/sync", nil, true))
	if synced["status"] != "Paid" {
		t.Fatal("sync did not complete payment", synced)
	}
	wallet := asMap(request("GET", "/api/wallet", nil, "", true))
	if wallet["earned"].(float64) <= 300000 {
		t.Fatal("synced payment missing from wallet", wallet)
	}

	zip := request("GET", "/api/galleries/"+id+"/free-download", nil, "", false)
	if zip.StatusCode != 200 || !strings.HasPrefix(zip.Header.Get("Content-Type"), "application/zip") {
		t.Fatal("free zip failed", zip.Status)
	}
	zip.Body.Close()
	check(jsonReq("PATCH", "/api/photos/"+pending, map[string]any{"price": 200}, true), 200)
	check(request("GET", "/api/photos/"+pending+"/free", nil, "", false), 404)
	check(request("GET", "/api/photos/"+photo+"/clean?token="+url.QueryEscape(token), nil, "", false), 200)
	check(request("GET", "/api/photos/"+photo+"/clean?size=thumb&token="+url.QueryEscape(token), nil, "", false), 200)
	// Removal during checkout keeps the original files and order available for a late success.
	late := asMap(jsonReq("POST", "/api/checkout", map[string]any{"galleryId": id, "email": "late@example.com", "photos": []string{photo}, "expectedAmount": 300000}, false))
	lateID, lateToken := late["order"].(string), late["token"].(string)
	check(request("DELETE", "/api/photos/"+photo, nil, "", true), 204)
	check(jsonReq("POST", "/api/checkout", map[string]any{"galleryId": id, "email": "other@example.com", "photos": []string{photo}}, false), 400)
	paidLate := asMap(jsonReq("POST", "/api/checkout/"+lateID+"/verify", map[string]any{"token": lateToken}, false))
	if paidLate["status"] != "Paid" {
		t.Fatal("archived photo was not deliverable", paidLate)
	}
	linkLate := asMap(jsonReq("POST", "/api/photos/"+photo+"/download-link", map[string]any{"token": lateToken}, false))
	check(request("GET", linkLate["url"].(string), nil, "", false), 200)
	check(request("DELETE", "/api/galleries/"+id, nil, "", true), 204)
	check(jsonReq("POST", "/api/checkout", map[string]any{"galleryId": id, "email": "other@example.com", "photos": []string{pending}}, false), 400)
	check(request("GET", "/api/galleries/"+id, nil, "", false), 200)
	check(request("GET", "/api/photos/"+photo+"/clean?token="+url.QueryEscape(lateToken), nil, "", false), 200)

}
