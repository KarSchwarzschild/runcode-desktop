package desktop

import "encoding/base64"

// imageGenJWT is an unsigned token whose claims give tokenIdentity a stable
// account fingerprint; the gateway, not this client, authenticates real tokens.
func imageGenJWT(sub string) string {
	return "x." + base64.RawURLEncoding.EncodeToString([]byte(`{"iss":"image-generation-test","sub":"`+sub+`"}`)) + ".x"
}
