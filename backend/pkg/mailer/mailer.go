// Package mailer provides small SMTP helpers for account verification emails.
package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"mime"
	"net"
	"net/smtp"
	"strings"
	"time"

	"github.com/kleinai/backend/pkg/config"
)

type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

type Mailer struct {
	cfg config.SMTP
}

func New(cfg config.SMTP) *Mailer {
	if cfg.Port == 0 {
		cfg.Port = 465
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}
	if strings.TrimSpace(cfg.FromEmail) == "" {
		cfg.FromEmail = cfg.Username
	}
	return &Mailer{cfg: cfg}
}

func (m *Mailer) Disabled() bool {
	if m == nil {
		return true
	}
	return strings.TrimSpace(m.cfg.Host) == "" ||
		m.cfg.Port == 0 ||
		strings.TrimSpace(m.cfg.Username) == "" ||
		strings.TrimSpace(m.cfg.Password) == "" ||
		strings.TrimSpace(m.cfg.FromEmail) == ""
}

func (m *Mailer) Send(ctx context.Context, msg Message) error {
	if m.Disabled() {
		return fmt.Errorf("smtp not configured")
	}
	to := strings.TrimSpace(msg.To)
	if to == "" {
		return fmt.Errorf("mail recipient required")
	}
	addr := fmt.Sprintf("%s:%d", m.cfg.Host, m.cfg.Port)
	raw := m.buildMessage(msg)
	auth := smtp.PlainAuth("", m.cfg.Username, m.cfg.Password, m.cfg.Host)

	if m.cfg.UseSSL {
		dialer := &net.Dialer{Timeout: m.cfg.Timeout}
		conn, err := tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{
			ServerName:         m.cfg.Host,
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: false,
		})
		if err != nil {
			return err
		}
		defer conn.Close()
		return m.sendWithClient(ctx, conn, auth, []string{to}, raw)
	}

	dialer := &net.Dialer{Timeout: m.cfg.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	defer conn.Close()
	return m.sendWithClient(ctx, conn, auth, []string{to}, raw)
}

func (m *Mailer) sendWithClient(ctx context.Context, conn net.Conn, auth smtp.Auth, recipients []string, raw []byte) error {
	done := make(chan error, 1)
	go func() {
		c, err := smtp.NewClient(conn, m.cfg.Host)
		if err != nil {
			done <- err
			return
		}
		defer c.Close()
		if m.cfg.UseStartTLS {
			if ok, _ := c.Extension("STARTTLS"); ok {
				if err := c.StartTLS(&tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}); err != nil {
					done <- err
					return
				}
			}
		}
		if ok, _ := c.Extension("AUTH"); ok {
			if err := c.Auth(auth); err != nil {
				done <- err
				return
			}
		}
		if err := c.Mail(m.cfg.FromEmail); err != nil {
			done <- err
			return
		}
		for _, rcpt := range recipients {
			if err := c.Rcpt(rcpt); err != nil {
				done <- err
				return
			}
		}
		w, err := c.Data()
		if err != nil {
			done <- err
			return
		}
		if _, err := w.Write(raw); err != nil {
			_ = w.Close()
			done <- err
			return
		}
		if err := w.Close(); err != nil {
			done <- err
			return
		}
		done <- c.Quit()
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func (m *Mailer) buildMessage(msg Message) []byte {
	var b bytes.Buffer
	fromName := strings.TrimSpace(m.cfg.FromName)
	from := strings.TrimSpace(m.cfg.FromEmail)
	if fromName != "" {
		from = fmt.Sprintf("%s <%s>", mime.QEncoding.Encode("utf-8", fromName), from)
	}
	contentType := "text/plain; charset=UTF-8"
	body := msg.Text
	if strings.TrimSpace(msg.HTML) != "" {
		contentType = "text/html; charset=UTF-8"
		body = msg.HTML
	}
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + strings.TrimSpace(msg.To) + "\r\n")
	b.WriteString("Subject: " + mime.QEncoding.Encode("utf-8", msg.Subject) + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: " + contentType + "\r\n")
	b.WriteString("\r\n")
	b.WriteString(body)
	return b.Bytes()
}

func RenderVerificationCode(scene, code string, expiresMinutes int) (string, string) {
	action := "完成邮箱验证"
	if scene == "reset_password" {
		action = "重置登录密码"
	}
	subject := "DAPO 达波显影验证码"
	html := fmt.Sprintf(`<!doctype html><html><body style="margin:0;padding:28px;background:#0b0f0d;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#f6fff8;">
<div style="max-width:520px;margin:auto;border:1px solid rgba(190,255,209,.22);border-radius:24px;padding:28px;background:linear-gradient(135deg,rgba(255,255,255,.08),rgba(255,255,255,.02));">
<div style="font-size:22px;font-weight:600;letter-spacing:.02em;">DAPO 达波显影</div>
<p style="margin:18px 0 8px;color:#b9c8be;font-size:15px;">请使用以下验证码%s：</p>
<div style="font-size:38px;letter-spacing:.28em;font-weight:700;color:#d2ffb6;margin:20px 0;">%s</div>
<p style="margin:0;color:#b9c8be;font-size:14px;">验证码 %d 分钟内有效。若不是你本人操作，可以忽略这封邮件。</p>
</div></body></html>`, action, code, expiresMinutes)
	return subject, html
}
