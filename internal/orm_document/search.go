package orm_document

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
)

// DocumentWithScore holds the doc-level search-relevant fields plus BM25
// score. Avoids leaking internal columns (file_key, status, error, ...) into
// search responses.
type DocumentWithScore struct {
	ID          uuid.UUID       `gorm:"column:id" json:"doc_id"`
	Filename    string          `gorm:"column:filename" json:"filename"`
	Title       string          `gorm:"column:title" json:"title"`
	Description string          `gorm:"column:description" json:"description"`
	Metadata    map[string]any  `gorm:"column:metadata;serializer:json" json:"metadata"`
	Score       float64         `gorm:"column:score" json:"score"`
}

// SearchDocuments performs BM25 search over document-level text
// (filename + title + description) via pg_search. Used to find documents
// by topic/title/summary rather than by section content.
//
//	folder_id scope:
//	  recursive=false → documents directly in that folder
//	  recursive=true  → documents in folder + all descendant folders
//
// metadata is optional — when non-empty, documents whose metadata column
// does not contain all the given key-value pairs are excluded.
//
// Soft-deleted rows are excluded (gorm.Raw does not auto-apply the
// DeletedAt filter, so we add it explicitly).
func SearchDocuments(ctx context.Context, query string, folder_id uuid.UUID, recursive bool, metadata map[string]any, limit int) ([]DocumentWithScore, error) {
	if limit <= 0 {
		limit = 10
	}

	var conditions []string
	var args []interface{}

	conditions = append(conditions, "d @@@ paradedb.parse(?)")
	args = append(args, query)

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
		SELECT d.id, d.filename, d.title, d.description, d.metadata,
		       paradedb.score(d) AS score
		FROM documents d
		WHERE ` + strings.Join(conditions, " AND ") + `
		ORDER BY score DESC
		LIMIT ?`

	var rows []DocumentWithScore
	err := dao.GetDB().WithContext(ctx).Raw(sql, args...).Scan(&rows).Error
	return rows, err
}
