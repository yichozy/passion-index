package models

import "github.com/google/uuid"

// ChatMessage is one turn of a chat conversation (role: user/assistant).
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCitation is one validated citation tag from the answer: the
// section/page range actually read by the tool loop that backs the claim.
type ChatCitation struct {
	Filename  string     `json:"filename"`
	PageStart int        `json:"page_start"`
	PageEnd   int        `json:"page_end"`
	NodeID    *uuid.UUID `json:"node_id,omitempty"`
	Snippet   string     `json:"snippet,omitempty"`
}

// ChatResult is the chat answer plus trace-validated citations.
type ChatResult struct {
	Answer    string         `json:"answer"`
	Citations []ChatCitation `json:"citations"`
}
