package models

// Figure is an image attached to a Node.
//
// Named after the file basename (e.g. "f4d4cd06...e8da.jpg") — populated
// directly from Popo build_tree output (img_path field, propagated by
// MinerU-Popo after #11 fix).
//
// Data is base64-encoded image bytes; populated on demand by the resolver
// layer when the client requests image data.
//
// Summary is intentionally absent: leaf Node.Summary already integrates
// figure content via vision input, so per-figure summaries are redundant.
type Figure struct {
	Name    string `json:"name"`
	Page    int    `json:"page"`              // 0-based physical page index
	Data    string `json:"data,omitempty"`    // base64; populated on demand
	Caption string `json:"caption,omitempty"` // from MinerU image_caption / table_caption
}
