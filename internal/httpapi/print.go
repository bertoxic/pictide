package httpapi

import (
	"encoding/base64"
	"html/template"
	"net/http"

	qrcode "github.com/skip2/go-qrcode"
	"pictide.local/studio/internal/mongostore"
)

var printCardTemplate = template.Must(template.New("print").Parse(`<!doctype html><html><head><meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} — print card</title><style>@page{size:{{.Size}};margin:0}*{box-sizing:border-box}body{margin:0;background:{{.Background}};color:{{.Ink}};font-family:Georgia,serif;text-align:center}.card{min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:10mm}.brand{font:12px Arial;letter-spacing:.3em}h1{font-size:32px;margin:12px}p{font:14px Arial;line-height:1.6}.qr{width:48mm;height:48mm;padding:3mm}button{margin:20px;padding:10px 20px}@media print{button{display:none}.card{height:100vh}}</style></head><body><main class="card"><div class="brand">PICTIDE · YOUR MOMENTS</div><h1>{{.Title}}</h1><p>{{.Message}}</p><img class="qr" src="{{.QR}}" alt="Gallery QR code"><p>Made to be remembered.</p><button onclick="window.print()">Print or save as PDF</button></main></body></html>`))

func (s *Server) printGallery(w http.ResponseWriter, r *http.Request, user *account, id string) {
	store, _ := mongostore.NewGalleries(s.db, user.ID)
	g, err := store.Get(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	layouts := map[string]string{"card": "148mm 105mm", "a4": "210mm 297mm", "sticker": "80mm 80mm", "story": "108mm 192mm"}
	themes := map[string][2]string{"minimalist": {"#ffffff", "#282c23"}, "luxury": {"#24251f", "#e4d2a7"}, "botanical": {"#edf0e3", "#4f5b2a"}, "monolith": {"#292929", "#ffffff"}}
	size := layouts[r.URL.Query().Get("layout")]
	if size == "" {
		size = layouts["card"]
	}
	theme, ok := themes[r.URL.Query().Get("theme")]
	if !ok {
		theme = themes["minimalist"]
	}
	title := r.URL.Query().Get("title")
	if title == "" {
		title = stringValue(g["share_title"])
	}
	if title == "" {
		title = stringValue(g["name"])
	}
	message := r.URL.Query().Get("message")
	if message == "" {
		message = stringValue(g["share_message"])
	}
	if message == "" {
		message = "Scan to view your photographs and keep the moments you love."
	}
	if len(title) > 200 || len(message) > 500 {
		fail(w, 400, "Print text is too long.")
		return
	}
	fg := r.URL.Query().Get("qr_color")
	if fg == "" {
		fg = stringValue(g["share_color"])
	}
	bg := r.URL.Query().Get("qr_background")
	if bg == "" {
		bg = stringValue(g["share_qr_background"])
	}
	if !cardColor.MatchString(fg) || !cardColor.MatchString(bg) {
		fail(w, 400, "Invalid QR color.")
		return
	}
	base := s.cfg.PublicURL
	if base == "" {
		if s.secure() {
			base = "https://" + r.Host
		} else {
			base = "http://" + r.Host
		}
	}
	q, err := qrcode.New(base+"/l/"+id, qrcode.Medium)
	if err != nil {
		fail(w, 500, "Could not generate QR code.")
		return
	}
	q.ForegroundColor = qrColor(fg)
	q.BackgroundColor = qrColor(bg)
	png, err := q.PNG(800)
	if err != nil {
		fail(w, 500, "Could not generate QR code.")
		return
	}
	data := struct {
		Title, Message        string
		Size, Background, Ink template.CSS
		QR                    template.URL
	}{title, message, template.CSS(size), template.CSS(theme[0]), template.CSS(theme[1]), template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = printCardTemplate.Execute(w, data)
}
