package chat_service

// The chat tool surface, the six PageIndex cloud tools (see
// pageindex_prompt.txt): get_folder_structure, browse_documents
// (time | relevance), search_documents (keyword), get_document_structure,
// get_page_content, get_document_image. get_document, get_section and
// search_sections below are kept implemented but not exposed — PageIndex's
// chat has no such tools. Everything runs in-process; document-derived
// text passes llm_safety.Sanitize before entering the model's context.
// Error semantics: infrastructure failures return Go errors (the tool
// loop aborts); "not found"/"not ready" return {"error": ...} results
// so the model can adapt, per the prompt's protocol.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/aliyun"
	"github.com/yichozy/hopebox/tool_base"
	"github.com/yichozy/hopebox/utils"
	"github.com/yichozy/passion-index/internal/llm_safety"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"

	"github.com/yichozy/passion-index/services/document_search_service"
	"github.com/yichozy/passion-index/services/document_service"
	"github.com/yichozy/passion-index/services/folder_service"
)

// trace_entry records a section/page a tool actually read — the citation
// validator in chat.go matches answer tags against these.
type trace_entry struct {
	filename   string
	node_id    uuid.UUID
	page_start int
	page_end   int
	snippet    string
}

// tool_set carries per-conversation state: the folder scope and the read
// trace. The tool loop executes sequentially, no locking needed.
type tool_set struct {
	folder_id *uuid.UUID
	trace     []trace_entry
}

// sanitized marshals v to JSON and redacts injection phrases — every
// document-derived tool result goes through this before the model sees it.
func sanitized(v any) (string, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return llm_safety.Sanitize(string(raw)), nil
}

// error_result is a soft failure the model can adapt to.
func error_result(message string) *tool_base.QueryResult {
	out, _ := json.Marshal(map[string]string{"error": message})
	return &tool_base.QueryResult{Return: string(out)}
}

// arg_folder_id resolves a tool's optional folder_id, falling back to
// the conversation scope.
func (t *tool_set) arg_folder_id(args map[string]any) *uuid.UUID {
	raw, _ := args["folder_id"].(string)
	if id, err := uuid.Parse(raw); err == nil {
		return &id
	}
	return t.folder_id
}

// definitions assembles the six cloud-aligned tools keyed by name for
// ChatKwargs.Tools.
func (t *tool_set) definitions() map[string]tool_base.ToolDefinition {
	return map[string]tool_base.ToolDefinition{
		"get_folder_structure":   t.get_folder_structure(),
		"browse_documents":       t.browse_documents(),
		"search_documents":       t.search_documents(),
		"get_document_structure": t.get_document_structure(),
		"get_page_content":       t.get_page_content(),
		"get_document_image":     t.get_document_image(),
		// Kept but not exposed — PageIndex's chat has no such tools.
		// "get_document":        t.get_document(),
		// "get_section":         t.get_section(),
		// "search_sections":     t.search_sections(),
	}
}

func (t *tool_set) get_folder_structure() tool_base.ToolDefinition {
	type args struct {
		FolderID string `json:"folder_id,omitempty" schema_description:"Optional folder UUID to scope to a subtree. Omit for the whole library"`
		Depth    int    `json:"depth,omitempty" schema_description:"Max nesting level to expand, 1-10. Default 10"`
	}
	return tool_base.ToolDefinition{
		Definition: tool_base.FunctionDefinition{
			Name:        "get_folder_structure",
			Description: "Orientation step — the folder hierarchy as a tree with document/subfolder counts. Call FIRST when exploring the library.",
			Parameters:  utils.GenerateSchema[args](),
		},
		Function: func(ctx context.Context, m map[string]any) (*tool_base.QueryResult, error) {
			depth := 10
			if v, ok := m["depth"].(float64); ok && int(v) > 0 {
				depth = int(v)
			}
			if depth > 10 {
				return error_result("depth must be 1-10"), nil
			}
			roots, err := folder_service.GetFolderTree(ctx, t.arg_folder_id(m), depth)
			if err != nil {
				return nil, err
			}
			out, err := sanitized(roots)
			if err != nil {
				return nil, err
			}
			return &tool_base.QueryResult{Return: out}, nil
		},
	}
}

func (t *tool_set) browse_documents() tool_base.ToolDefinition {
	type args struct {
		FolderID  string `json:"folder_id,omitempty" schema_description:"Optional folder UUID. Omit to browse the whole library"`
		Recursive bool   `json:"recursive,omitempty" schema_description:"false = direct contents plus child-folder hints; true = flatten all descendants"`
		Sort      string `json:"sort,omitempty" schema_description:"\"time\" (default, newest first) or \"relevance\" (semantic ranking, requires query)"`
		Query     string `json:"query,omitempty" schema_description:"Natural-language query — required when sort=relevance"`
		Limit     int    `json:"limit,omitempty" schema_description:"Number of documents, 1-50. Default 10"`
		Offset    int    `json:"offset,omitempty" schema_description:"Pagination offset — time mode only"`
	}
	return tool_base.ToolDefinition{
		Definition: tool_base.FunctionDefinition{
			Name: "browse_documents",
			Description: "Primary document discovery. sort=time lists by upload date (newest first, with " +
				"child-folder hints); sort=relevance ranks by semantic similarity to a natural-language query " +
				"(matches meaning, synonyms, brand names — \"Opdivo\" finds documents that only say \"nivolumab\").",
			Parameters: utils.GenerateSchema[args](),
		},
		Function: func(ctx context.Context, m map[string]any) (*tool_base.QueryResult, error) {
			sort, _ := m["sort"].(string)
			query, _ := m["query"].(string)
			if sort == "" {
				sort = "time"
			}
			if sort != "time" && sort != "relevance" {
				return error_result("sort must be \"time\" or \"relevance\""), nil
			}
			if sort == "relevance" && query == "" {
				return error_result("sort=relevance requires a query"), nil
			}
			recursive, _ := m["recursive"].(bool)
			limit := 10
			if v, ok := m["limit"].(float64); ok && int(v) > 0 {
				limit = int(v)
			}

			if sort == "relevance" {
				rows, err := document_search_service.SearchDocuments(ctx, query, t.arg_folder_id(m),
					recursive, nil, limit, document_search_service.SEARCH_MODE_SEMANTIC)
				if err != nil {
					return nil, err
				}
				out, err := sanitized(map[string]any{"sort": "relevance", "documents": rows})
				if err != nil {
					return nil, err
				}
				return &tool_base.QueryResult{Return: out}, nil
			}

			offset := 0
			if v, ok := m["offset"].(float64); ok && int(v) > 0 {
				offset = int(v)
			}
			docs, total, err := orm_document.ListDocumentsByFolder(ctx, t.arg_folder_id(m),
				recursive, limit, offset)
			if err != nil {
				return nil, err
			}
			items := make([]map[string]any, len(docs))
			for i := range docs {
				items[i] = map[string]any{
					"id":          docs[i].ID,
					"filename":    docs[i].Filename,
					"title":       docs[i].Title,
					"description": docs[i].Description,
					"status":      docs[i].Status,
					"created_at":  docs[i].CreatedAt,
				}
			}
			out, err := sanitized(map[string]any{"sort": "time", "documents": items, "total": total})
			if err != nil {
				return nil, err
			}
			return &tool_base.QueryResult{Return: out}, nil
		},
	}
}

func (t *tool_set) search_documents() tool_base.ToolDefinition {
	type args struct {
		Query     string `json:"query" schema_description:"Exact keywords only: NCT IDs, drug codes, acronyms, filenames — NOT a natural-language sentence"`
		FolderID  string `json:"folder_id,omitempty" schema_description:"Optional folder UUID"`
		Recursive bool   `json:"recursive,omitempty" schema_description:"Include descendant folders"`
		Limit     int    `json:"limit,omitempty" schema_description:"Number of documents, 1-50. Default 10"`
	}
	return tool_base.ToolDefinition{
		Definition: tool_base.FunctionDefinition{
			Name:        "search_documents",
			Description: "ESCALATION: keyword (BM25) search over filename/title/description. Only when browse_documents(sort=relevance) missed a document you have strong reason to believe exists.",
			Parameters:  utils.GenerateSchema[args](),
		},
		Function: func(ctx context.Context, m map[string]any) (*tool_base.QueryResult, error) {
			query, _ := m["query"].(string)
			if query == "" {
				return error_result("query is required"), nil
			}
			recursive, _ := m["recursive"].(bool)
			limit := 10
			if v, ok := m["limit"].(float64); ok && int(v) > 0 {
				limit = int(v)
			}
			rows, err := document_search_service.SearchDocuments(ctx, query, t.arg_folder_id(m),
				recursive, nil, limit, document_search_service.SEARCH_MODE_KEYWORD)
			if err != nil {
				return nil, err
			}
			out, err := sanitized(map[string]any{"documents": rows})
			if err != nil {
				return nil, err
			}
			return &tool_base.QueryResult{Return: out}, nil
		},
	}
}

// Kept but not exposed in definitions — PageIndex's chat has no such tool.
func (t *tool_set) get_document() tool_base.ToolDefinition {
	type args struct {
		DocID string `json:"doc_id" schema_description:"Document UUID"`
	}
	return tool_base.ToolDefinition{
		Definition: tool_base.FunctionDefinition{
			Name:        "get_document",
			Description: "A document's metadata and processing status. Status pipeline: PENDING → OCR → STRUCTURING → SUMMARY → EMBEDDING → DONE, or FAILED.",
			Parameters:  utils.GenerateSchema[args](),
		},
		Function: func(ctx context.Context, m map[string]any) (*tool_base.QueryResult, error) {
			raw, _ := m["doc_id"].(string)
			id, err := uuid.Parse(raw)
			if err != nil {
				return error_result("bad doc_id"), nil
			}
			doc, err := orm_document.GetDocumentByID(ctx, id)
			if err != nil {
				return nil, err
			}
			if doc.ID == uuid.Nil {
				return error_result(fmt.Sprintf("document %s not found", id)), nil
			}
			out, err := sanitized(map[string]any{
				"id": doc.ID, "filename": doc.Filename, "title": doc.Title,
				"description": doc.Description, "status": doc.Status, "page_count": doc.PageCount,
			})
			if err != nil {
				return nil, err
			}
			return &tool_base.QueryResult{Return: out}, nil
		},
	}
}

func (t *tool_set) get_document_structure() tool_base.ToolDefinition {
	type args struct {
		DocID string `json:"doc_id" schema_description:"Document UUID"`
	}
	return tool_base.ToolDefinition{
		Definition: tool_base.FunctionDefinition{
			Name:        "get_document_structure",
			Description: "A document's structure as a nested JSON tree — every section with title, node id, page range, summary, and children (PageIndex cloud shape). Read BEFORE page content for documents over 20 pages; section summaries let you pick the right pages without extra calls.",
			Parameters:  utils.GenerateSchema[args](),
		},
		Function: func(ctx context.Context, m map[string]any) (*tool_base.QueryResult, error) {
			raw, _ := m["doc_id"].(string)
			id, err := uuid.Parse(raw)
			if err != nil {
				return error_result("bad doc_id"), nil
			}
			tree, err := document_service.GetDocumentStructure(ctx, id)
			if err != nil {
				if errors.Is(err, document_service.ErrDocumentNotFound) ||
					errors.Is(err, document_service.ErrDocumentNotReady) {
					return error_result(err.Error()), nil
				}
				return nil, err
			}
			// Summaries ride in the tree, so reading the structure IS a
			// content read — trace every node for citation validation.
			if doc, err := orm_document.GetDocumentByID(ctx, id); err == nil {
				var walk func(node *models.Node)
				walk = func(node *models.Node) {
					if node.ID != uuid.Nil {
						t.trace = append(t.trace, trace_entry{
							filename:   doc.Filename,
							node_id:    node.ID,
							page_start: node.PageStart,
							page_end:   node.PageEnd,
							snippet:    node.Summary,
						})
					}
					for i := range node.Nodes {
						walk(&node.Nodes[i])
					}
				}
				if tree != nil {
					walk(tree)
				}
			}
			out, err := sanitized(map[string]any{"doc_id": id, "structure": tree})
			if err != nil {
				return nil, err
			}
			return &tool_base.QueryResult{Return: out}, nil
		},
	}
}

func (t *tool_set) get_page_content() tool_base.ToolDefinition {
	type args struct {
		DocID string `json:"doc_id" schema_description:"Document UUID"`
		Pages string `json:"pages" schema_description:"Page spec, 1-based: \"5\" or \"3,7,10\" or \"5-10\""`
	}
	return tool_base.ToolDefinition{
		Definition: tool_base.FunctionDefinition{
			Name:        "get_page_content",
			Description: "Read the raw markdown text of specific pages — page-granular ground truth. Pick pages from get_document_structure; keep ranges tight. For a whole section in full, use get_section.",
			Parameters:  utils.GenerateSchema[args](),
		},
		Function: func(ctx context.Context, m map[string]any) (*tool_base.QueryResult, error) {
			raw, _ := m["doc_id"].(string)
			id, err := uuid.Parse(raw)
			if err != nil {
				return error_result("bad doc_id"), nil
			}
			pages_spec, _ := m["pages"].(string)
			pages, err := document_service.GetPageText(ctx, id, pages_spec)
			if err != nil {
				if errors.Is(err, document_service.ErrDocumentNotFound) ||
					errors.Is(err, document_service.ErrDocumentNotReady) ||
					errors.Is(err, document_service.ErrBadPageSpec) {
					return error_result(err.Error()), nil
				}
				return nil, err
			}
			filename := ""
			if doc, err := orm_document.GetDocumentByID(ctx, id); err == nil {
				filename = doc.Filename
			}
			for i := range pages {
				t.trace = append(t.trace, trace_entry{
					filename:   filename,
					page_start: pages[i].Page - 1,
					page_end:   pages[i].Page - 1,
					snippet:    first_line(pages[i].Text),
				})
			}
			out, err := sanitized(map[string]any{"doc_id": id, "filename": filename, "pages": pages})
			if err != nil {
				return nil, err
			}
			return &tool_base.QueryResult{Return: out}, nil
		},
	}
}

// first_line extracts a short snippet from page text (empty when absent).
func first_line(text string) string {
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		return text[:idx]
	}
	return text
}

// Kept but not exposed in definitions — PageIndex's chat has no such tool.
func (t *tool_set) get_section() tool_base.ToolDefinition {
	type args struct {
		NodeID string `json:"node_id" schema_description:"Section UUID — from the get_document_structure tree or search results"`
	}
	return tool_base.ToolDefinition{
		Definition: tool_base.FunctionDefinition{
			Name:        "get_section",
			Description: "One section in full: summary, complete text, figures, child section titles.",
			Parameters:  utils.GenerateSchema[args](),
		},
		Function: func(ctx context.Context, m map[string]any) (*tool_base.QueryResult, error) {
			raw, _ := m["node_id"].(string)
			id, err := uuid.Parse(raw)
			if err != nil {
				return error_result("bad node_id"), nil
			}
			node, err := document_service.GetSection(ctx, id)
			if err != nil {
				if errors.Is(err, document_service.ErrSectionNotFound) {
					return error_result(err.Error()), nil
				}
				return nil, err
			}
			out, err := sanitized(map[string]any{
				"node_id": node.ID, "doc_id": node.DocID, "title": node.Title,
				"page_start": node.PageStart, "page_end": node.PageEnd,
				"summary": node.Summary, "text": node.Text, "figures": node.Figures,
			})
			if err != nil {
				return nil, err
			}
			return &tool_base.QueryResult{Return: out}, nil
		},
	}
}

func (t *tool_set) get_document_image() tool_base.ToolDefinition {
	type args struct {
		DocID     string `json:"doc_id" schema_description:"Document UUID"`
		ImageName string `json:"image_name" schema_description:"Figure file name — from a section's figures list"`
	}
	return tool_base.ToolDefinition{
		Definition: tool_base.FunctionDefinition{
			Name:        "get_document_image",
			Description: "A figure's page, caption, and a fetchable URL (base64 bytes are NOT returned — the URL serves the image directly).",
			Parameters:  utils.GenerateSchema[args](),
		},
		Function: func(ctx context.Context, m map[string]any) (*tool_base.QueryResult, error) {
			raw, _ := m["doc_id"].(string)
			id, err := uuid.Parse(raw)
			if err != nil {
				return error_result("bad doc_id"), nil
			}
			name, _ := m["image_name"].(string)
			rows, err := orm_node.GetByDocID(ctx, id)
			if err != nil {
				return nil, err
			}
			for i := range rows {
				for _, figure := range rows[i].Figures {
					if figure.Name != name {
						continue
					}
					oss, err := aliyun.NewOss()
					if err != nil {
						return nil, fmt.Errorf("oss init: %w", err)
					}
					url, err := oss.GetObjectURL(ctx, fmt.Sprintf("passion-index/%s/images/%s", id, name))
					if err != nil {
						return nil, err
					}
					out, err := sanitized(map[string]any{
						"name": figure.Name, "page": figure.Page,
						"caption": figure.Caption, "url": url,
					})
					if err != nil {
						return nil, err
					}
					return &tool_base.QueryResult{Return: out}, nil
				}
			}
			return error_result(fmt.Sprintf("figure %q not found in document %s", name, id)), nil
		},
	}
}

// Kept but not exposed in definitions — PageIndex's chat has no such tool.
func (t *tool_set) search_sections() tool_base.ToolDefinition {
	type args struct {
		DocID string `json:"doc_id" schema_description:"Document UUID"`
		Query string `json:"query" schema_description:"Natural-language query about the section content"`
	}
	return tool_base.ToolDefinition{
		Definition: tool_base.FunctionDefinition{
			Name:        "search_sections",
			Description: "Strongest in-document retrieval: meaning-based section search (tree search) inside ONE document. Prefer it when the target document is already known.",
			Parameters:  utils.GenerateSchema[args](),
		},
		Function: func(ctx context.Context, m map[string]any) (*tool_base.QueryResult, error) {
			raw, _ := m["doc_id"].(string)
			id, err := uuid.Parse(raw)
			if err != nil {
				return error_result("bad doc_id"), nil
			}
			query, _ := m["query"].(string)
			if query == "" {
				return error_result("query is required"), nil
			}
			hit, err := document_search_service.SearchDocumentNodes(ctx, id, query)
			if err != nil {
				return nil, err
			}
			if hit == nil {
				return error_result("no matching section found"), nil
			}
			out, err := sanitized(map[string]any{
				"node_id": hit.ID, "doc_id": hit.DocID, "filename": hit.Filename,
				"title": hit.Title, "page_start": hit.PageStart, "page_end": hit.PageEnd,
				"summary": hit.Summary, "score": hit.Score,
			})
			if err != nil {
				return nil, err
			}
			return &tool_base.QueryResult{Return: out}, nil
		},
	}
}
