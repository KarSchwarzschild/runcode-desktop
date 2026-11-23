package desktop

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strings"
	"time"
)

// mailSMTPClient 是可替换的 SMTP 探测/自测发信接口；正式实现不发 DATA 以外的副作用。
type mailSMTPClient interface {
	// TestLogin 连接 → TLS → AUTH → QUIT，不发送邮件。
	TestLogin(ctx context.Context, host string, port int, username, password string) error
	// SendSelfTest 向同一地址发固定测试信；结果 accepted/failed/unknown。
	SendSelfTest(ctx context.Context, host string, port int, username, password, subject, body string) (status, detail string, err error)
}

// 测试可替换；生产默认走真实隐式 TLS 465。
var mailSMTP mailSMTPClient = realMailSMTP{}

type realMailSMTP struct{}

func (realMailSMTP) TestLogin(ctx context.Context, host string, port int, username, password string) error {
	client, err := dialSMTPTLS(ctx, host, port)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	return client.Quit()
}

func (realMailSMTP) SendSelfTest(ctx context.Context, host string, port int, username, password, subject, body string) (string, string, error) {
	client, err := dialSMTPTLS(ctx, host, port)
	if err != nil {
		return "failed", err.Error(), err
	}
	defer func() { _ = client.Close() }()
	if err := client.Auth(smtp.PlainAuth("", username, password, host)); err != nil {
		return "failed", err.Error(), err
	}
	if err := client.Mail(username); err != nil {
		return "failed", err.Error(), err
	}
	if err := client.Rcpt(username); err != nil {
		return "failed", err.Error(), err
	}
	w, err := client.Data()
	if err != nil {
		return "failed", err.Error(), err
	}
	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		username, username, subject, body)
	if _, err := w.Write([]byte(msg)); err != nil {
		_ = w.Close()
		return "unknown", err.Error(), err
	}
	if err := w.Close(); err != nil {
		// DATA 最终应答丢失：无法证明未提交，归 unknown，禁止自动重发。
		return "unknown", err.Error(), err
	}
	if err := client.Quit(); err != nil {
		return "accepted", "quit: " + err.Error(), nil
	}
	return "accepted", "", nil
}

func dialSMTPTLS(ctx context.Context, host string, port int) (*smtp.Client, error) {
	addr := fmt.Sprintf("%s:%d", host, port)
	d := net.Dialer{Timeout: 8 * time.Second}
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	deadline, ok := ctx.Deadline()
	if ok {
		_ = raw.SetDeadline(deadline)
	}
	tlsConn := tls.Client(raw, &tls.Config{
		ServerName: host,
		MinVersion: tls.VersionTLS12,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, fmt.Errorf("tls: %w", err)
	}
	client, err := smtp.NewClient(tlsConn, host)
	if err != nil {
		_ = tlsConn.Close()
		return nil, err
	}
	return client, nil
}

func classifyMailSMTPError(err error) (category, message string) {
	if err == nil {
		return "", ""
	}
	msg := err.Error()
	lower := strings.ToLower(msg)
	switch {
	case strings.Contains(lower, "tls") || strings.Contains(lower, "certificate") || strings.Contains(lower, "x509"):
		return "tls", "安全连接失败：请检查系统时间与企业网络，不能忽略证书继续。"
	case strings.Contains(lower, "auth") || strings.Contains(lower, "535") || strings.Contains(lower, "534"):
		return "auth", "认证失败：请确认使用的是网页生成的专用安全密码，而不是网页登录密码。"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "i/o timeout") || strings.Contains(lower, "connection refused") || strings.Contains(lower, "no such host"):
		return "network", "无法连接发信服务器：请核对邮箱版本、网络与出口 TCP 465。"
	default:
		return "unknown", "连接或发信失败，请按可能原因依次排查。"
	}
}
