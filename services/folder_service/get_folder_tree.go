package folder_service

import (
	"context"
	"sort"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/utils"
	"github.com/yichozy/passion-index/graph/types"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_folder"
)

// GetFolderTree returns folder(s) as a nested tree with batched counts.
//
//	folderID nil → each top-level folder as its own root (forest)
//	folderID set → single tree rooted at that folder
//	depth       → max nesting level to expand
//
// Three SQL round-trips: GetSubtree (recursive CTE) + two batched GROUP BY
// count queries. Tree assembly (stitching the flat folder list into a graph
// of FolderNode) is the service's job, not the orm's.
func GetFolderTree(ctx context.Context, folderID *uuid.UUID, depth int) ([]*types.FolderNode, error) {
	folders, err := orm_folder.GetSubtree(ctx, folderID, depth)
	if err != nil {
		return nil, err
	}
	if len(folders) == 0 {
		return []*types.FolderNode{}, nil
	}

	folder_ids := make([]uuid.UUID, len(folders))
	for i := range folders {
		folder_ids[i] = folders[i].ID
	}
	subfolder_counts, err := orm_folder.CountByParentIDs(ctx, folder_ids)
	if err != nil {
		return nil, err
	}
	document_counts, err := orm_document.CountByFolderIDs(ctx, folder_ids)
	if err != nil {
		return nil, err
	}

	// Build node lookup keyed by folder ID.
	nodes_by_id := make(map[uuid.UUID]*types.FolderNode, len(folders))
	for i := range folders {
		folder := folders[i]
		var node types.FolderNode
		_ = utils.CopyObj(&folder, &node)
		node.DocumentCount = int(document_counts[folder.ID])
		node.FolderCount = int(subfolder_counts[folder.ID])
		node.Folders = []*types.FolderNode{}
		nodes_by_id[folder.ID] = &node
	}

	// Link each node to its parent, or promote to root when:
	//   - parent_id is nil (top-level), or
	//   - it is the anchor itself (folderID set), or
	//   - its parent isn't in scope (defensive — shouldn't normally trigger).
	var roots []*types.FolderNode
	for i := range folders {
		folder := folders[i]
		node := nodes_by_id[folder.ID]
		switch {
		case folder.ParentID == nil, folderID != nil && folder.ID == *folderID:
			roots = append(roots, node)
		default:
			if parent, ok := nodes_by_id[*folder.ParentID]; ok {
				parent.Folders = append(parent.Folders, node)
			} else {
				roots = append(roots, node)
			}
		}
	}

	// Sort for stable output.
	sort.Slice(roots, func(a, b int) bool { return roots[a].Name < roots[b].Name })
	for _, node := range nodes_by_id {
		sort.Slice(node.Folders, func(a, b int) bool { return node.Folders[a].Name < node.Folders[b].Name })
	}

	return roots, nil
}
