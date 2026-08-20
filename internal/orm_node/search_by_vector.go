package orm_node

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// SearchNodesByVector recalls the top_k embedded nodes most similar to
// query_vec (a pgvector literal), scoped like SearchNodes: folder subtree
// when recursive, metadata containment, soft-deletes excluded. Only
// DocID and Score are populated in each hit — callers aggregate per
// document (see models.NodeWithScore).
//
//	folder_id scope:
//	  nil             → all documents (virtual root, whole library)
//	  recursive=false → documents directly in that folder
//	  recursive=true  → documents in folder + all descendant folders
//
// Placeholder order in the final SQL: the SELECT's similarity expression
// comes first, then the WHERE conditions, then ORDER BY and LIMIT — args
// must be appended in exactly that order.
func SearchNodesByVector(ctx context.Context, query_vec string, folder_id *uuid.UUID, recursive bool, metadata map[string]any, top_k int) ([]models.NodeWithScore, error) {
	if top_k <= 0 {
		top_k = 100
	}

	var conditions []string
	var args []interface{}

	args = append(args, query_vec)

	conditions = append(conditions, "n.embedding IS NOT NULL")
	conditions = append(conditions, "n.deleted_at IS NULL")
	conditions = append(conditions, "d.deleted_at IS NULL")

	switch {
	case folder_id == nil:
		// whole library — no folder scoping
	case recursive:
		conditions = append(conditions, `d.folder_id IN (
			WITH RECURSIVE subtree AS (
				SELECT id FROM folders WHERE id = ? AND deleted_at IS NULL
				UNION ALL
				SELECT f.id FROM folders f JOIN subtree s ON f.parent_id = s.id
				WHERE f.deleted_at IS NULL
			)
			SELECT id FROM subtree
		)`)
	default:
		conditions = append(conditions, "d.folder_id = ?")
	}
	if folder_id != nil {
		args = append(args, *folder_id)
	}

	if len(metadata) > 0 {
		metadataJSON, _ := json.Marshal(metadata)
		conditions = append(conditions, "d.metadata @> ?::jsonb")
		args = append(args, string(metadataJSON))
	}

	args = append(args, query_vec, top_k)

	sql := `
		SELECT n.doc_id,
		       1 - (n.embedding <=> ?::vector) AS score
		FROM nodes n
		JOIN documents d ON n.doc_id = d.id
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY n.embedding <=> ?::vector
		LIMIT ?`

	var rows []models.NodeWithScore
	err := dao.GetDB().WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
}
