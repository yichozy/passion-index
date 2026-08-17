package document_search_service

// Semantic node retrieval on a single document tree — service layer only
// (no GraphQL exposure), mirroring ConDB's retriever shape: the caller
// picks WHICH tree (doc_id), this finds THE node answering the query.
// Cross-doc selection is the caller's job (e.g. BM25 SearchDocuments).

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

// SearchDocumentNodes runs semantic retrieval on one document's tree and
// returns its single best node (select_k=1): BeamSearch deep (drills to
// leaf) for trees ≤ BLOCK_SWITCH_NODE_COUNT nodes, BlockSearch above.
// Score is the LLM's 0-10 relevance. Any failure fails loudly — no BM25
// degradation.
func SearchDocumentNodes(ctx context.Context, doc_id uuid.UUID, query string) (*orm_node.NodeWithScore, error) {
	rows, err := orm_node.GetByDocID(ctx, doc_id)
	if err != nil {
		return nil, fmt.Errorf("load tree (doc=%s): %w", doc_id, err)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("semantic node search (doc=%s): document has no nodes", doc_id)
	}
	root := models.AssembleTree(rows)

	var node *models.Node
	var score float64
	if len(rows) > BLOCK_SWITCH_NODE_COUNT {
		result, err := BlockSearch(ctx, root, query)
		if err != nil {
			return nil, fmt.Errorf("block search (doc=%s): %w", doc_id, err)
		}
		node, score = result.Node, result.Score
	} else {
		result, err := BeamSearch(ctx, root, query)
		if err != nil {
			return nil, fmt.Errorf("beam search (doc=%s): %w", doc_id, err)
		}
		node, score = result.Node, result.Score
	}

	doc, err := orm_document.GetDocumentByID(ctx, doc_id)
	if err != nil {
		return nil, fmt.Errorf("load doc (doc=%s): %w", doc_id, err)
	}
	return &orm_node.NodeWithScore{
		Node:     *node,
		Filename: doc.Filename,
		Score:    score,
	}, nil
}
