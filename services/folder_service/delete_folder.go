package folder_service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_folder"
)

// ErrFolderNotEmpty is returned by DeleteFolder when documents still live
// in the folder — callers must move or delete them first. orm_folder.Delete
// itself is a plain delete-by-PK and refuses nothing; this service-level
// guard is the only thing keeping docs from being orphaned.
var ErrFolderNotEmpty = errors.New("folder not empty")

func DeleteFolder(ctx context.Context, id uuid.UUID) error {
	counts, err := orm_document.CountByFolderIDs(ctx, []uuid.UUID{id})
	if err != nil {
		return err
	}
	if counts[id] > 0 {
		return fmt.Errorf("%w: folder %s contains %d document(s); remove them before deleting",
			ErrFolderNotEmpty, id, counts[id])
	}
	return orm_folder.Delete(ctx, id)
}
