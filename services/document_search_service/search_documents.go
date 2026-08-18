package document_search_service

// SearchDocuments is the doc-level search entry dispatching on mode.
// Mode strings mirror the GraphQL SearchMode enum values so the resolver
// can pass them through verbatim.

import (
	"context"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_document"
)

const (
	SEARCH_MODE_SEMANTIC = "SEMANTIC"
	SEARCH_MODE_KEYWORD  = "KEYWORD"
)

// SearchDocuments ranks documents under folder_id by relevance to the
// query. mode=KEYWORD (or anything else, defaulting to SEMANTIC):
//
//	SEMANTIC → vector recall over node embeddings + DocScore aggregation
//	           (SearchDocumentsSemantic) — matches by meaning.
//	KEYWORD  → BM25 over doc-level text (filename + title + description,
//	           orm_document.SearchDocumentsBm25) — matches literal terms.
//
//	folder_id scope:
//	  recursive=false → documents directly in that folder
//	  recursive=true  → documents in folder + all descendant folders
func SearchDocuments(ctx context.Context, query string, folder_id uuid.UUID, recursive bool, metadata map[string]any, limit int, mode string) ([]orm_document.DocumentWithScore, error) {
	if mode == SEARCH_MODE_KEYWORD {
		return orm_document.SearchDocumentsBm25(ctx, query, folder_id, recursive, metadata, limit)
	}
	return SearchDocumentsSemantic(ctx, query, folder_id, recursive, metadata, limit)
}
