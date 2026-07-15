package statistics

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"strings"
)

// HashVisitor returns a project-isolated opaque identity. A logged-in user
// always wins over an anonymous device identity.
func HashVisitor(masterKey, projectSalt []byte, userID, anonymousID string) uint64 {
	userID = strings.TrimSpace(userID)
	anonymousID = strings.TrimSpace(anonymousID)
	identity := ""
	if userID != "" {
		identity = "u:" + userID
	} else if anonymousID != "" {
		identity = "a:" + anonymousID
	} else {
		return 0
	}
	mac := hmac.New(sha256.New, masterKey)
	_, _ = mac.Write(projectSalt)
	_, _ = mac.Write([]byte{0})
	_, _ = mac.Write([]byte(identity))
	return binary.BigEndian.Uint64(mac.Sum(nil)[:8])
}
