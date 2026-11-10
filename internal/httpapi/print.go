package httpapi

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"github.com/go-chi/chi/v5"
	qrcode "github.com/skip2/go-qrcode"
	"html/template"
	"image/color"
	"pictide.local/studio/internal/qr"
	"net/http"
	"strconv"
	"strings"
)

func parseColor(s string) color.RGBA {
	if len(s) != 7 {
		return color.RGBA{79, 91, 42, 255}
	}
	v, e := strconv.ParseUint(strings.TrimPrefix(s, "#"), 16, 32)
	if e != nil {
		return color.RGBA{79, 91, 42, 255}
	}
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 255}
}
func (s *Server) qrImage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	g, e := s.gallery(id)
	if e != nil {
		http.NotFound(w, r)
		return
	}
	c := r.URL.Query().Get("qr_color")
	if c == "" {
		c = str(g["share_color"])
		if c == "" {
			c = "#4F5B2A"
		}
	}
	bg := r.URL.Query().Get("qr_background")
	if bg == "" {
		bg = str(g["share_qr_background"])
		if bg == "" {
			bg = "#ffffff"
		}
	}
	if !cardColor.MatchString(c) || !cardColor.MatchString(bg) {
		fail(w, 400, "Invalid QR color.")
		return
	}
	link := s.base(r) + "/l/" + id
	if r.URL.Query().Get("format") == "png" {
		q, e := qrcode.New(link, qrcode.Medium)
		if e != nil {
			fail(w, 500, "Could not generate QR code.")
			return
		}
		q.ForegroundColor = parseColor(c)
		q.BackgroundColor = parseColor(bg)
		b, e := q.PNG(1200)
		if e != nil {
			fail(w, 500, "Could not generate QR code.")
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Write(b)
		return
	}
	b, e := qr.SVGWithBackground(link, c, bg)
	if e != nil {
		fail(w, 400, "Invalid QR color.")
		return
	}
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Write(b)
}

var printTemplate = template.Must(template.New("print").Parse(`<!doctype html><html><head><title>{{.Title}} — print card</title><style>@page{size:{{.Size}};margin:0}*{box-sizing:border-box}body{margin:0;background:{{.Background}};color:{{.Ink}};font-family:Georgia,serif;text-align:center}.card{min-height:100vh;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:10mm}.brand{font:12px Arial;letter-spacing:.3em}h1{font-size:32px;margin:12px}p{font:14px Arial;line-height:1.6}.qr{width:48mm;height:48mm;background:white;padding:3mm}button{margin:20px;padding:10px 20px}@media print{button{display:none}.card{height:100vh}}</style></head><body><main class="card"><div class="brand">PICTIDE · YOUR MOMENTS</div><h1>{{.Title}}</h1><p>{{.Message}}</p><img class="qr" src="{{.QR}}" alt="Gallery QR code"><p>Made to be remembered.</p><button onclick="window.print()">Print or save as PDF</button></main></body></html>`))

func (s *Server) printCard(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	g, e := s.gallery(id)
	if e != nil {
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
		title = str(g["share_title"])
	}
	if title == "" {
		title = str(g["name"])
	}
	message := r.URL.Query().Get("message")
	if message == "" {
		message = str(g["share_message"])
	}
	if message == "" {
		message = "Scan to view your photographs and keep the moments you love."
	}
	q, e := qrcode.New(s.base(r)+"/l/"+id, qrcode.Medium)
	if e != nil {
		fail(w, 500, "Could not generate print card.")
		return
	}
	fg := r.URL.Query().Get("qr_color")
	if fg == "" {
		fg = str(g["share_color"])
	}
	bg := r.URL.Query().Get("qr_background")
	if bg == "" {
		bg = str(g["share_qr_background"])
	}
	if bg == "" {
		bg = "#ffffff"
	}
	if !cardColor.MatchString(fg) || !cardColor.MatchString(bg) {
		fail(w, 400, "Invalid QR color.")
		return
	}
	q.ForegroundColor = parseColor(fg)
	q.BackgroundColor = parseColor(bg)
	png, e := q.PNG(800)
	if e != nil {
		fail(w, 500, "Could not generate print card.")
		return
	}
	data := struct {
		Title, Message        string
		Size, Background, Ink template.CSS
		QR                    template.URL
	}{title, message, template.CSS(size), template.CSS(theme[0]), template.CSS(theme[1]), template.URL("data:image/png;base64," + base64.StdEncoding.EncodeToString(png))}
	var out bytes.Buffer
	if e = printTemplate.Execute(&out, data); e != nil {
		fail(w, 500, "Could not generate print card.")
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, out.String())
}
