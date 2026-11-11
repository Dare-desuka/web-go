package telegram

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/gotd/td/tg"
)

// Locator serializes a Telegram document address for
// movie_parts.telegram_file_id: tgdoc:<docID>:<accessHash>:<hexFileRef>.
// File references expire; DownloadRange refetches them from the message.
func EncodeLocator(doc *tg.Document) string {
	return "tgdoc:" + strconv.FormatInt(doc.ID, 10) + ":" +
		strconv.FormatInt(doc.AccessHash, 10) + ":" +
		hex.EncodeToString(doc.FileReference)
}

// DecodeLocator parses EncodeLocator output.
func DecodeLocator(s string) (docID, accessHash int64, fileRef []byte, err error) {
	parts := strings.Split(s, ":")
	if len(parts) != 4 || parts[0] != "tgdoc" {
		return 0, 0, nil, fmt.Errorf("bad locator")
	}
	// ponytail: no values echoed back; locator fragments are internal IDs.
	if docID, err = strconv.ParseInt(parts[1], 10, 64); err != nil || docID == 0 {
		return 0, 0, nil, fmt.Errorf("bad locator")
	}
	if accessHash, err = strconv.ParseInt(parts[2], 10, 64); err != nil {
		return 0, 0, nil, fmt.Errorf("bad locator")
	}
	if fileRef, err = hex.DecodeString(parts[3]); err != nil {
		return 0, 0, nil, fmt.Errorf("bad locator")
	}
	return docID, accessHash, fileRef, nil
}
