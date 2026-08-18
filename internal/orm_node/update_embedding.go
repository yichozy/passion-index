package orm_node

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/dao"
)

// UpdateEmbedding writes node embeddings for one document in a
// single UPDATE ... FROM (VALUES ...) statement (atomic on its own — no
// transaction needed). vecs maps node id → pgvector text literal
// ("[0.1,0.2,...]" — encoding/json on a []float64 produces exactly
// that). The embedding column is managed by raw SQL (see
// orm/create_document_indexes.go), not a GORM model field.
func UpdateEmbedding(ctx context.Context, doc_id uuid.UUID, vecs map[uuid.UUID]string) error {
	values := make([]string, 0, len(vecs))
	args := make([]interface{}, 0, len(vecs)*2+1)
	for node_id, vec := range vecs {
		values = append(values, "(?::uuid, ?::text)")
		args = append(args, node_id, vec)
	}
	args = append(args, doc_id)

	sql := `
		UPDATE nodes AS n
		SET embedding = v.vec::vector
		FROM (VALUES ` + strings.Join(values, ",") + `) AS v(id, vec)
		WHERE n.id = v.id AND n.doc_id = ?`

	return dao.GetDB().WithContext(ctx).Exec(sql, args...).Error
}
