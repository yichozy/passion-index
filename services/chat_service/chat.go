package chat_service

// Chat is the document-QA entry: an LLM tool loop over the in-process
// document tools. Non-streaming — the caller blocks for the whole
// conversation (tens of seconds with retrieval).

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/llm"
	"github.com/yichozy/hopebox/llm_types"
	"github.com/yichozy/passion-index/models"
)

const (
	CHAT_TIMEOUT    = 300 * time.Second
	CHAT_MAX_ROUNDS = 30 // tool-loop rounds; the discovery protocol needs headroom
)

// citation_tag matches [filename p.X] tags (single page, 1-based) in answers.
var citation_tag = regexp.MustCompile(`\[([^\[\]]+?) p\.(\d+)\]`)

// Chat runs the conversation: everything before the last user message
// becomes history, the last user message is the prompt.
//
// folder_id scopes discovery to a subtree (nil = whole library).
func Chat(ctx context.Context, messages []models.ChatMessage, folder_id *uuid.UUID) (*models.ChatResult, error) {
	if len(messages) == 0 {
		return nil, fmt.Errorf("messages is required")
	}
	last := messages[len(messages)-1]
	if last.Role != "user" || strings.TrimSpace(last.Content) == "" {
		return nil, fmt.Errorf("last message must be a non-empty user message")
	}

	model := os.Getenv("PASSION_INDEX_CHAT_MODEL")
	if model == "" {
		model = llm_types.DeepSeekV4Pro
	}

	tools := &tool_set{folder_id: folder_id}
	chat_ctx, cancel := context.WithTimeout(ctx, CHAT_TIMEOUT)
	defer cancel()

	history := make([]llm_types.Message, 0, len(messages)-1)
	for _, message := range messages[:len(messages)-1] {
		history = append(history, llm_types.Message{Role: message.Role, Content: message.Content})
	}

	max_rounds := CHAT_MAX_ROUNDS
	resp, err := llm.Chat(chat_ctx, llm_types.ChatKwargs{
		SystemPrompt:     CHAT_PROMPT,
		UserPrompt:       last.Content,
		HistoryMsgs:      history,
		ModelName:        model,
		Tools:            tools.definitions(),
		MaxNumOfAutoCall: &max_rounds,
	}, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}

	// Map answer tags to trace entries the tool loop actually read.
	// Filename matching is forgiving (case-insensitive, ".pdf"-insensitive,
	// either-direction substring) because models shorten filenames; the tag
	// page (1-based) must fall inside the read range. Deduped by node
	// (node_id) or filename+pages (page-level reads), order of first
	// appearance. Citation pages come from the trace, not the tag.
	var citations []models.ChatCitation
	seen_keys := map[string]bool{}
	key_of := func(entry trace_entry) string {
		if entry.node_id != uuid.Nil {
			return entry.node_id.String()
		}
		return fmt.Sprintf("%s:%d-%d", entry.filename, entry.page_start, entry.page_end)
	}
	for _, match := range citation_tag.FindAllStringSubmatch(resp.Content, -1) {
		tag_filename := strings.ToLower(strings.TrimSpace(match[1]))
		tag_filename = strings.TrimSuffix(tag_filename, ".pdf")
		page, _ := strconv.Atoi(match[2])
		for i := range tools.trace {
			entry_filename := strings.ToLower(tools.trace[i].filename)
			entry_filename = strings.TrimSuffix(entry_filename, ".pdf")
			if tag_filename == "" ||
				(!strings.Contains(entry_filename, tag_filename) && !strings.Contains(tag_filename, entry_filename)) {
				continue
			}
			if page < tools.trace[i].page_start+1 || page > tools.trace[i].page_end+1 { // tags are 1-based, trace pages 0-based
				continue
			}
			if seen_keys[key_of(tools.trace[i])] {
				break
			}
			seen_keys[key_of(tools.trace[i])] = true
			citation := models.ChatCitation{
				Filename:  tools.trace[i].filename,
				PageStart: tools.trace[i].page_start + 1,
				PageEnd:   tools.trace[i].page_end + 1,
				Snippet:   tools.trace[i].snippet,
			}
			if tools.trace[i].node_id != uuid.Nil {
				node_id := tools.trace[i].node_id
				citation.NodeID = &node_id
			}
			citations = append(citations, citation)
			break
		}
	}

	return &models.ChatResult{
		Answer:    resp.Content,
		Citations: citations,
	}, nil
}
