package platform

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"pictide.local/studio/internal/security"
)

func emailAddress(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	a, e := mail.ParseAddress(value)
	if e != nil || a.Address != value || len(value) > 254 || strings.ContainsAny(value, "\r\n") {
		return ""
	}
	return value
}
func (p *Platform) smtpMail(to, subject, body string) error {
	from := emailAddress(p.cfg.SMTPFrom)
	if from == "" || emailAddress(to) == "" {
		return fmt.Errorf("invalid mail address")
	}
	conn, e := net.DialTimeout("tcp", net.JoinHostPort(p.cfg.SMTPHost, p.cfg.SMTPPort), 10*time.Second)
	if e != nil {
		return e
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	c, e := smtp.NewClient(conn, p.cfg.SMTPHost)
	if e != nil {
		return e
	}
	defer c.Close()
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return fmt.Errorf("SMTP server must support STARTTLS")
	}
	if e = c.StartTLS(&tls.Config{ServerName: p.cfg.SMTPHost, MinVersion: tls.VersionTLS12}); e != nil {
		return e
	}
	if p.cfg.SMTPUser != "" {
		if e = c.Auth(smtp.PlainAuth("", p.cfg.SMTPUser, p.cfg.SMTPPassword, p.cfg.SMTPHost)); e != nil {
			return e
		}
	}
	if e = c.Mail(from); e != nil {
		return e
	}
	if e = c.Rcpt(to); e != nil {
		return e
	}
	w, e := c.Data()
	if e != nil {
		return e
	}
	_, e = fmt.Fprintf(w, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n", from, to, subject, body)
	if e != nil {
		return e
	}
	if e = w.Close(); e != nil {
		return e
	}
	return c.Quit()
}
func (p *Platform) limit(r *http.Request, kind string) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	key := kind + ":" + host
	p.mu.Lock()
	defer p.mu.Unlock()
	a := p.attempts[key]
	if time.Now().After(a.until) {
		a = attempt{until: time.Now().Add(time.Hour)}
	}
	if a.count >= 5 {
		return false
	}
	a.count++
	p.attempts[key] = a
	return true
}
func (p *Platform) issueLink(id, email, kind string) error {
	token, e := security.Token()
	if e != nil {
		return e
	}
	tx, e := p.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if _, e = tx.Exec("DELETE FROM account_links WHERE account_id=? AND kind=?", id, kind); e != nil {
		return e
	}
	if _, e = tx.Exec("INSERT INTO account_links VALUES(?,?,?,?,?)", security.HashToken(token), id, email, kind, time.Now().Add(30*time.Minute).Unix()); e != nil {
		return e
	}
	if e = tx.Commit(); e != nil {
		return e
	}
	title := "Verify your Pictide email"
	if kind == "reset" {
		title = "Reset your Pictide password"
	}
	link := p.cfg.PublicURL + "/#" + kind + "=" + token
	return p.sendMail(email, title, title+"\n\n"+link+"\n\nThis link expires in 30 minutes and can be used once. If you did not request this, you can ignore this message.")
}
func (p *Platform) register(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if p.sendMail == nil {
		fail(w, 503, "Registration is unavailable until email delivery is configured.")
		return
	}
	if !p.limit(r, "register") {
		fail(w, 429, "Too many attempts. Try again later.")
		return
	}
	var b struct{ Username, Email, Password string }
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	b.Username = strings.ToLower(strings.TrimSpace(b.Username))
	b.Email = emailAddress(b.Email)
	if !regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,63}$`).MatchString(b.Username) || b.Email == "" || len(b.Password) < 12 || len(b.Password) > 200 {
		fail(w, 400, "Enter a valid username, email, and password of at least 12 characters.")
		return
	}
	a, e := p.createStudio(b.Username, b.Password, b.Email)
	if e != nil {
		fail(w, 409, "Unable to create account. Username or email may already be in use.")
		return
	}
	if e = p.issueLink(a.id, b.Email, "verify"); e != nil {
		log.Printf("verification email delivery failed: %v", e)
	}
	reply(w, 201, map[string]string{"message": "Check your email for a verification link. If it does not arrive, request another link."})
}
func (p *Platform) forgot(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if p.sendMail == nil {
		fail(w, 503, "Email recovery is not configured.")
		return
	}
	if !p.limit(r, "recovery") {
		fail(w, 429, "Too many attempts. Try again later.")
		return
	}
	var b struct{ Email string }
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	email := emailAddress(b.Email)
	var id string
	var verified, disabled int
	if email != "" && p.db.QueryRow("SELECT id,email_verified,disabled FROM accounts WHERE email=? COLLATE NOCASE", email).Scan(&id, &verified, &disabled) == nil && disabled == 0 {
		kind := "reset"
		if verified == 0 {
			kind = "verify"
		}
		if e := p.issueLink(id, email, kind); e != nil {
			log.Printf("account email delivery failed: %v", e)
		}
	}
	reply(w, 200, map[string]string{"message": "If an eligible account exists, a link has been sent."})
}
func (p *Platform) bindEmail(w http.ResponseWriter, r *http.Request, s *studio) {
	if s == nil {
		fail(w, 401, "Please sign in.")
		return
	}
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if p.sendMail == nil {
		fail(w, 503, "Email delivery is not configured.")
		return
	}
	if !p.limit(r, "email") {
		fail(w, 429, "Too many attempts. Try again later.")
		return
	}
	var b struct{ Email, Password string }
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b) != nil {
		fail(w, 400, "Invalid request.")
		return
	}
	email := emailAddress(b.Email)
	var hash string
	if email == "" || p.db.QueryRow("SELECT password_hash FROM accounts WHERE id=?", s.id).Scan(&hash) != nil || !security.CheckPassword(hash, b.Password) {
		fail(w, 400, "Enter a valid email and your current password.")
		return
	}
	if e := p.issueLink(s.id, email, "verify"); e != nil {
		fail(w, 502, "Could not send verification email.")
		return
	}
	reply(w, 200, map[string]string{"message": "Check your email to verify this address."})
}
func (p *Platform) consumeLink(w http.ResponseWriter, r *http.Request, kind string) {
	if r.Method != "POST" {
		w.WriteHeader(405)
		return
	}
	if !p.limit(r, "consume-"+kind) {
		fail(w, 429, "Too many attempts. Try again later.")
		return
	}
	var b struct{ Token, Password string }
	if json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&b) != nil || len(b.Token) != 64 || (kind == "reset" && (len(b.Password) < 12 || len(b.Password) > 200)) {
		fail(w, 400, "Invalid or expired link.")
		return
	}
	var hash string
	var e error
	if kind == "reset" {
		hash, e = security.PasswordHash(b.Password)
		if e != nil {
			fail(w, 500, "Could not reset password.")
			return
		}
	}
	if kind == "reset" {
		p.mu.Lock()
		defer p.mu.Unlock()
	}
	tx, e := p.db.Begin()
	if e != nil {
		fail(w, 500, "Could not process link.")
		return
	}
	defer tx.Rollback()
	var id, email string
	if e = tx.QueryRow("SELECT account_id,email FROM account_links WHERE token_hash=? AND kind=? AND expires>?", security.HashToken(b.Token), kind, time.Now().Unix()).Scan(&id, &email); e != nil {
		fail(w, 400, "Invalid or expired link.")
		return
	}
	version := uuid.NewString()
	if kind == "verify" {
		var result interface{ RowsAffected() (int64, error) }
		result, e = tx.Exec("UPDATE accounts SET email=?,email_verified=1 WHERE id=? AND disabled=0", email, id)
		if e == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				e = fmt.Errorf("account unavailable")
			}
		}
	} else {
		var result interface{ RowsAffected() (int64, error) }
		result, e = tx.Exec("UPDATE accounts SET password_hash=?,session_version=? WHERE id=? AND email=? AND email_verified=1 AND disabled=0", hash, version, id, email)
		if e == nil {
			n, _ := result.RowsAffected()
			if n != 1 {
				e = fmt.Errorf("account unavailable")
			}
		}
	}
	if e == nil {
		_, e = tx.Exec("DELETE FROM account_links WHERE account_id=? AND (kind=? OR ?='reset')", id, kind, kind)
	}
	if e == nil {
		e = tx.Commit()
	}
	if e != nil {
		fail(w, 400, "This link cannot be used. The email may already belong to another account.")
		return
	}
	if kind == "reset" {
		if s := p.studios[id]; s != nil {
			s.version = version
		}
	}
	reply(w, 200, map[string]string{"message": "Account updated. You can sign in now."})
}
