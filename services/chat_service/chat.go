package chat_service

// Chat is the document-QA entry: an LLM tool loop over the in-process
// document tools, ending in a cited answer. Non-streaming — the caller
// blocks for the whole conversation (tens of seconds with retrieval).

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

// Chat runs the conversation: everything before the last user message
// becomes history, the last user message is the prompt.
//
// folder_id scopes discovery to a subtree (nil = whole library).
// The answer's [filename p.X-Y] tags are validated against the sections
// the tool loop actually read — dangling tags are dropped from the
// citations.
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

	tools := new_tool_set(folder_id)
	chat_ctx, cancel := context.WithTimeout(ctx, CHAT_TIMEOUT)
	defer cancel()

	history := make([]llm_types.Message, 0, len(messages)-1)
	for _, message := range messages[:len(messages)-1] {
		history = append(history, llm_types.Message{Role: message.Role, Content: message.Content})
	}

	resp, err := llm.Chat(chat_ctx, llm_types.ChatKwargs{
		SystemPrompt:     CHAT_PROMPT,
		UserPrompt:       last.Content,
		HistoryMsgs:      history,
		ModelName:        model,
		Tools:            tools.definitions(),
		MaxNumOfAutoCall: intPtr(CHAT_MAX_ROUNDS),
	}, nil, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("chat: %w", err)
	}

	return &models.ChatResult{
		Answer:    resp.Content,
		Citations: validate_citations(resp.Content, tools.trace),
	}, nil
}

func intPtr(v int) *int { return &v }

// citation_tag matches [filename p.X] or [filename p.X-Y] in answers.
var citation_tag = regexp.MustCompile(`\[([^\[\]]+?) p\.(\d+)(?:-(\d+))?\]`)

// validate_citations maps answer tags to trace entries the tool loop
// actually read. Filename matching is forgiving (case-insensitive,
// ".pdf"-insensitive, either-direction substring) because models
// shorten filenames; page numbers must overlap the read section.
// Deduped by node, order of first appearance.
func validate_citations(answer string, trace []trace_entry) []models.ChatCitation {
	var citations []models.ChatCitation
	// Dedup key: the node for section-level reads, filename+pages for
	// page-level reads (node_id is uuid.Nil there).
	seen_keys := map[string]bool{}
	key_of := func(entry trace_entry) string {
		if entry.node_id != uuid.Nil {
			return entry.node_id.String()
		}
		return fmt.Sprintf("%s:%d-%d", entry.filename, entry.page_start, entry.page_end)
	}
	for _, match := range citation_tag.FindAllStringSubmatch(answer, -1) {
		tag_filename := strings.ToLower(strings.TrimSpace(match[1]))
		tag_filename = strings.TrimSuffix(tag_filename, ".pdf")
		start, _ := strconv.Atoi(match[2])
		end := start
		if match[3] != "" {
			end, _ = strconv.Atoi(match[3])
		}
		for i := range trace {
			entry_filename := strings.ToLower(trace[i].filename)
			entry_filename = strings.TrimSuffix(entry_filename, ".pdf")
			if tag_filename == "" ||
				(!strings.Contains(entry_filename, tag_filename) && !strings.Contains(tag_filename, entry_filename)) {
				continue
			}
			if end < trace[i].page_start+1 || start > trace[i].page_end+1 { // tags are 1-based, trace pages 0-based
				continue
			}
			if seen_keys[key_of(trace[i])] {
				break
			}
			seen_keys[key_of(trace[i])] = true
			citation := models.ChatCitation{
				Filename:  trace[i].filename,
				PageStart: trace[i].page_start + 1,
				PageEnd:   trace[i].page_end + 1,
				Snippet:   trace[i].snippet,
			}
			if trace[i].node_id != uuid.Nil {
				node_id := trace[i].node_id
				citation.NodeID = &node_id
			}
			citations = append(citations, citation)
			break
		}
	}
	return citations
}
