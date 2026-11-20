package mail

import (
	"fmt"
	"net/mail"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	maxSubjectRunes = 200
	maxBodyBytes    = 256 * 1024
	maxAddrPerMsg   = 100
)

var (
	// htmlTagRe 检测用户试图写入原生 HTML 标签（邮件正文只允许 Markdown）。
	htmlTagRe = regexp.MustCompile(`(?is)<\s*/?\s*[a-z][^>]*>`)
	// headerInjectRe 拒绝主题/显示名中的换行与冒号头注入。
	headerInjectRe = regexp.MustCompile(`[\r\n]|:\s`)
)

// NormalizeAddress 解析并规范化单个邮箱；拒绝空地址与头注入。
func NormalizeAddress(name, address string) (MailAddress, error) {
	name = strings.TrimSpace(name)
	address = strings.TrimSpace(address)
	if address == "" {
		return MailAddress{}, fmt.Errorf("%w: empty address", ErrInvalid)
	}
	if headerInjectRe.MatchString(name) || headerInjectRe.MatchString(address) {
		return MailAddress{}, fmt.Errorf("%w: header injection", ErrInvalid)
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil {
		// 允许纯地址无尖括号。
		parsed, err = mail.ParseAddress(fmt.Sprintf("<%s>", address))
		if err != nil {
			return MailAddress{}, fmt.Errorf("%w: bad address", ErrInvalid)
		}
	}
	addr := strings.ToLower(strings.TrimSpace(parsed.Address))
	if addr == "" || strings.ContainsAny(addr, "\r\n") {
		return MailAddress{}, fmt.Errorf("%w: bad address", ErrInvalid)
	}
	display := name
	if display == "" {
		display = strings.TrimSpace(parsed.Name)
	}
	if headerInjectRe.MatchString(display) {
		return MailAddress{}, fmt.Errorf("%w: header injection", ErrInvalid)
	}
	return MailAddress{Name: display, Address: addr}, nil
}

// DedupeAddresses 按 Address 去重，保留首次出现顺序；同址不同名不猜合并显示名。
func DedupeAddresses(in []MailAddress) []MailAddress {
	seen := make(map[string]struct{}, len(in))
	out := make([]MailAddress, 0, len(in))
	for _, a := range in {
		key := strings.ToLower(strings.TrimSpace(a.Address))
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		a.Address = key
		out = append(out, a)
	}
	return out
}

// RejectNativeHTML 拒绝正文中的原生 HTML 标签。
func RejectNativeHTML(body string) error {
	if htmlTagRe.MatchString(body) {
		return fmt.Errorf("%w: native HTML not allowed in body", ErrInvalid)
	}
	return nil
}

// ValidateDraftEdit 校验可保存字段；individual 不允许 Cc/Bcc。
func ValidateDraftEdit(edit DraftEdit) error {
	subject := strings.TrimSpace(edit.Subject)
	if subject == "" {
		return fmt.Errorf("%w: empty subject", ErrInvalid)
	}
	if utf8.RuneCountInString(subject) > maxSubjectRunes {
		return fmt.Errorf("%w: subject too long", ErrInvalid)
	}
	if headerInjectRe.MatchString(subject) {
		return fmt.Errorf("%w: header injection in subject", ErrInvalid)
	}
	body := strings.TrimSpace(edit.BodyMarkdown)
	if body == "" {
		return fmt.Errorf("%w: empty body", ErrInvalid)
	}
	if len(body) > maxBodyBytes {
		return fmt.Errorf("%w: body too large", ErrInvalid)
	}
	if err := RejectNativeHTML(edit.BodyMarkdown); err != nil {
		return err
	}
	switch edit.SendMode {
	case ModeTogether, ModeIndividual:
	default:
		return fmt.Errorf("%w: send mode", ErrInvalid)
	}
	switch edit.DispatchMode {
	case ModeImmediate, ModeScheduledAt:
	default:
		return fmt.Errorf("%w: dispatch mode", ErrInvalid)
	}
	to, err := normalizeList(edit.To)
	if err != nil {
		return err
	}
	if len(to) == 0 {
		return fmt.Errorf("%w: need recipients", ErrInvalid)
	}
	cc, err := normalizeList(edit.Cc)
	if err != nil {
		return err
	}
	bcc, err := normalizeList(edit.Bcc)
	if err != nil {
		return err
	}
	if edit.SendMode == ModeIndividual && (len(cc) > 0 || len(bcc) > 0) {
		return fmt.Errorf("%w: individual forbids Cc/Bcc", ErrInvalid)
	}
	total := len(to) + len(cc) + len(bcc)
	if total > maxAddrPerMsg {
		return fmt.Errorf("%w: too many addresses", ErrInvalid)
	}
	if strings.TrimSpace(edit.AccountID) == "" {
		return fmt.Errorf("%w: account required", ErrInvalid)
	}
	if edit.DispatchMode == ModeScheduledAt {
		if strings.TrimSpace(edit.ScheduledLocal) == "" || strings.TrimSpace(edit.TimeZone) == "" {
			return fmt.Errorf("%w: schedule requires local time and timezone", ErrInvalid)
		}
	}
	return nil
}

func normalizeList(in []MailAddress) ([]MailAddress, error) {
	out := make([]MailAddress, 0, len(in))
	for _, a := range in {
		n, err := NormalizeAddress(a.Name, a.Address)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return DedupeAddresses(out), nil
}

// ResolveAmbiguousContacts 同名多联系人时必须选择，禁止猜测。
func ResolveAmbiguousContacts(name string, candidates []Contact) (Contact, error) {
	name = strings.TrimSpace(name)
	var matches []Contact
	for _, c := range candidates {
		if strings.EqualFold(strings.TrimSpace(c.Name), name) {
			matches = append(matches, c)
		}
	}
	if len(matches) == 0 {
		return Contact{}, fmt.Errorf("%w: contact not found", ErrNotFound)
	}
	if len(matches) > 1 {
		return Contact{}, fmt.Errorf("%w: ambiguous contact name", ErrInvalid)
	}
	return matches[0], nil
}
