package orm_node

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// SearchNodesByBm25 performs BM25 search over node content (title/summary/text)
// via pg_search. Joins documents to apply folder scope + metadata filters
// and to fetch filename for context.
//
// Soft-deleted rows are excluded (gorm.Raw does not auto-apply the
// DeletedAt filter, so we add it explicitly).
//
//	folder_id scope:
//	  recursive=false → documents directly in that folder
//	  recursive=true  → documents in folder + all descendant folders
//
// metadata is optional — when non-empty, documents whose metadata column
// does not contain all the given key-value pairs are excluded.
func SearchNodesByBm25(ctx context.Context, query string, folder_id uuid.UUID, recursive bool, metadata map[string]any, limit int) ([]models.NodeWithScore, error) {
	if limit <= 0 {
		limit = 10
	}

	var conditions []string
	var args []interface{}

	conditions = append(conditions, "n @@@ paradedb.parse(?)")
	args = append(args, query)

	conditions = append(conditions, "n.deleted_at IS NULL")
	conditions = append(conditions, "d.deleted_at IS NULL")

	if recursive {
		conditions = append(conditions, `d.folder_id IN (
			WITH RECURSIVE subtree AS (
				SELECT id FROM folders WHERE id = ? AND deleted_at IS NULL
				UNION ALL
				SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
				WHERE f.deleted_at IS NULL
			)
			SELECT id FROM subtree
		)`)
	} else {
		conditions = append(conditions, "d.folder_id = ?")
	}
	args = append(args, folder_id)

	if len(metadata) > 0 {
		metadataJSON, _ := json.Marshal(metadata)
		conditions = append(conditions, "d.metadata @> ?::jsonb")
		args = append(args, string(metadataJSON))
	}

	args = append(args, limit)

	sql := `
		SELECT n.id, n.doc_id, n.parent_id, n.title, n.summary, n.page_start, n.page_end,
		       d.filename,
		       paradedb.score(n) AS score
		FROM nodes n
		JOIN documents d ON n.doc_id = d.id
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY score DESC
		LIMIT ?`

	var rows []models.NodeWithScore
	err := dao.GetDB().WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
}
