package orm_node

import (
	"context"
	"fmt"
	"strings"

	"github.com/yichozy/hopebox/dao"
	"github.com/yichozy/passion-index/models"
)

// GetByPages returns all node rows covering any of the given pages.
// One SQL query with OR conditions — each row returned at most once.
func GetByPages(ctx context.Context, docID string, pages []int) ([]models.Node, error) {
	if len(pages) == 0 {
		return nil, nil
	}

	var conds []string
	var args []interface{}
	args = append(args, docID)
	for _, p := range pages {
		conds = append(conds, "(page_start <= ? AND page_end >= ?)")
		args = append(args, p, p)
	}
	query := fmt.Sprintf("doc_id = ? AND (%s)", strings.Join(conds, " OR "))

	var rows []models.Node
	err := dao.GetDB().WithContext(ctx).
		Where(query, args...).
		Order("id").
		Find(&rows).Error
	return rows, err
}
