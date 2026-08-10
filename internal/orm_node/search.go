package orm_node

import (
	"context"
	"strings"

	"github.com/lib/pq"
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

// Search performs full-text search on node title + summary + text.
// Joins documents table to apply metadata filters (doi, indication, study,
// literature_type) and retrieve filename in a single query.
func Search(ctx context.Context, query string, doc_ids []string, doi string, indication, study, literature_type []string, limit int) ([]NodeWithScore, error) {
	if limit <= 0 {
		limit = 10
	}

	var conditions []string
	var args []interface{}

	// Full-text search (always present)
	conditions = append(conditions, "n.search_vector @@ plainto_tsquery('english', ?)")
	args = append(args, query)

	// Optional doc_ids filter
	if len(doc_ids) > 0 {
		placeholders := make([]string, len(doc_ids))
		for i, id := range doc_ids {
			placeholders[i] = "?"
			args = append(args, id)
		}
		conditions = append(conditions, "n.doc_id IN ("+strings.Join(placeholders, ",")+")")
	}

	// Optional metadata filters (applied on documents table via JOIN)
	if doi != "" {
		conditions = append(conditions, "d.doi = ?")
		args = append(args, doi)
	}
	if len(indication) > 0 {
		conditions = append(conditions, "d.indication && ?")
		args = append(args, pq.Array(indication))
	}
	if len(study) > 0 {
		conditions = append(conditions, "d.study && ?")
		args = append(args, pq.Array(study))
	}
	if len(literature_type) > 0 {
		conditions = append(conditions, "d.literature_type && ?")
		args = append(args, pq.Array(literature_type))
	}

	args = append(args, query, limit)

	sql := `
		SELECT n.doc_id, n.id, n.parent_id, n.title, n.summary, n.page_start, n.page_end,
		       d.filename,
		       ts_rank_cd(n.search_vector, plainto_tsquery('english', ?)) AS score
		FROM nodes n
		JOIN documents d ON n.doc_id = d.id
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY score DESC
		LIMIT ?`

	var rows []NodeWithScore
	err := dao.GetDB().WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
}
