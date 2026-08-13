package orm_node

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// NodeWithScore holds a node row plus its search relevance score and the
// parent document's filename (joined from documents table).
type NodeWithScore struct {
	models.Node
	Filename string  `gorm:"column:filename" json:"filename"`
	Score    float64 `gorm:"column:score" json:"score"`
}

// Search performs BM25 search via pg_search (paradedb).
// Joins documents table to apply optional metadata filters (via JSONB @>
// containment) and retrieve filename in a single query.
//
// Soft-deleted rows are excluded (gorm.Raw does not auto-apply the
// DeletedAt filter, so we add it explicitly).
//
// metadata is optional — when non-empty, documents whose metadata column
// does not contain all the given key-value pairs are excluded.
func Search(ctx context.Context, query string, doc_ids []uuid.UUID, metadata map[string]any, limit int) ([]NodeWithScore, error) {
	if limit <= 0 {
		limit = 10
	}

	var conditions []string
	var args []interface{}

	// paradedb.parse takes a query string and matches across all indexed
	// text fields (title/summary/text). Default tokenizer is whitespace +
	// lowercase; multi-word queries OR-match by default, BM25 ranks.
	conditions = append(conditions, "n @@@ paradedb.parse(?)")
	args = append(args, query)

	// Soft-delete filter — gorm.Raw does not add this automatically.
	conditions = append(conditions, "n.deleted_at IS NULL")
	conditions = append(conditions, "d.deleted_at IS NULL")

	if len(doc_ids) > 0 {
		placeholders := make([]string, len(doc_ids))
		for i, id := range doc_ids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		conditions = append(conditions, "n.doc_id IN ("+strings.Join(placeholders, ",")+")")
	}

	if len(metadata) > 0 {
		metadataJSON, _ := json.Marshal(metadata)
		conditions = append(conditions, "d.metadata @> ?::jsonb")
		args = append(args, string(metadataJSON))
	}

	args = append(args, limit)

	sql := `
		SELECT n.doc_id, n.id, n.parent_id, n.title, n.summary, n.page_start, n.page_end,
		       d.filename,
		       paradedb.score(n) AS score
		FROM nodes n
		JOIN documents d ON n.doc_id = d.id
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY score DESC
		LIMIT ?`

	var rows []NodeWithScore
	err := dao.GetDB().WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
}
