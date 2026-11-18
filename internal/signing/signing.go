// Package signing signs approval decisions that come back from chat buttons.
//
// Chat callbacks are not signed by the chat server, so the core puts its own HMAC
// into the button context and checks it, together with the draft fingerprint.
package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
)

// Sign returns the HMAC of a decision on a wait for a draft fingerprint.
func Sign(secret []byte, waitID, fingerprint, decision string) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(waitID + "\x00" + fingerprint + "\x00" + decision))
	return hex.EncodeToString(mac.Sum(nil))
}

// Verify checks a signature in constant time.
func Verify(secret []byte, waitID, fingerprint, decision, sig string) bool {
	return hmac.Equal([]byte(Sign(secret, waitID, fingerprint, decision)), []byte(sig))
}
