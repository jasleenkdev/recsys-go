// internal/store/browse_cursor.go
package store

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// BrowseKey is the decoded form of the browse endpoint's opaque cursor:
// the (stars, id) sort key of the last row on the previous page, plus
// the language filter that page was computed under.
//
// The filter travels inside the cursor deliberately. Browse is a keyset
// scan, not a frozen snapshot, so a client that changed `language`
// halfway through paging would otherwise get a page seeked into the
// wrong result set. Carrying it here lets the handler detect the
// mismatch instead of silently returning nonsense.
type BrowseKey struct {
	Stars    int    `json:"stars"`
	ItemID   int64  `json:"item_id"`
	Language string `json:"language"`
}

// EncodeBrowseCursor base64-encodes a BrowseKey for use as an opaque
// API token.
func EncodeBrowseCursor(k BrowseKey) string {
	data, _ := json.Marshal(k) // BrowseKey is always marshalable
	return base64.URLEncoding.EncodeToString(data)
}

// DecodeBrowseCursor reverses EncodeBrowseCursor. Unlike the
// recommendations cursor — which can be silently discarded because a
// fresh snapshot is an equally valid answer — a bad browse cursor is
// surfaced to the caller as an error: restarting from the top of the
// catalog would look like working pagination while quietly repeating
// items the client already showed.
func DecodeBrowseCursor(raw string) (BrowseKey, error) {
	data, err := base64.URLEncoding.DecodeString(raw)
	if err != nil {
		return BrowseKey{}, fmt.Errorf("invalid cursor encoding: %w", err)
	}
	var k BrowseKey
	if err := json.Unmarshal(data, &k); err != nil {
		return BrowseKey{}, fmt.Errorf("invalid cursor contents: %w", err)
	}
	if k.ItemID <= 0 {
		return BrowseKey{}, fmt.Errorf("invalid cursor: missing item_id")
	}
	return k, nil
}
