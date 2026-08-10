package orm_node

import (
	"context"
	"strings"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// NodeWithScore holds a node row plus its search relevance score.
type NodeWithScore struct {
	models.Node
	Score float64 `gorm:"column:score" json:"score"`
}

// Search performs full-text search on node title + summary + text.
// If docIDs is non-empty, limits search to those documents.
// Returns nodes ordered by relevance (highest score first).
//
// Note: limit controls the number of NODE rows returned, not documents.
// The caller (document_service.SearchDocuments) groups these into documents
// and truncates. If one document has many matching nodes, fewer unique
// documents will appear in the final result — this is a known trade-off
// of the two-stage (DB group + Go aggregate) approach.
func Search(ctx context.Context, query string, doc_ids []string, limit int) ([]NodeWithScore, error) {
	if limit <= 0 {
		limit = 10
	}

	ts_query := "plainto_tsquery('english', ?)"
	args := []interface{}{query}

	doc_filter := ""
	if len(doc_ids) > 0 {
		placeholders := make([]string, len(doc_ids))
		for i, id := range doc_ids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		doc_filter = " AND doc_id IN (" + strings.Join(placeholders, ",") + ")"
	}

	// Explicit columns — skip text/figures (potentially large) since search
	// results only need doc_id, id, title, summary for display.
	// ts_rank_cd = cover density ranking: matched terms closer together = higher score.
	sql := `
		SELECT doc_id, id, parent_id, title, summary, page_start, page_end,
		       ts_rank_cd(search_vector, ` + ts_query + `) as score
		FROM nodes
		WHERE search_vector @@ ` + ts_query + doc_filter + `
		ORDER BY score DESC
		LIMIT ?`
	args = append(args, query, limit)

	var rows []NodeWithScore
	err := dao.GetDB().WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
}
