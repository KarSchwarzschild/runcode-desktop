package desktop

import (
	"encoding/base64"
	"encoding/json"
	"strings"
)

// tokenIdentityParts reads stable identity claims for local partitioning, not
// authentication. Callers retain ownership of token locking and epoch checks.
func tokenIdentityParts(token string) (issuer, subject string) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", ""
	}
	b, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", ""
	}
	var claims struct {
		Issuer  string `json:"iss"`
		Subject string `json:"sub"`
	}
	if json.Unmarshal(b, &claims) != nil || claims.Subject == "" {
		return "", ""
	}
	return claims.Issuer, claims.Subject
}
