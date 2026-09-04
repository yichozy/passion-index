package document_service

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/aliyun"
	hope_req "github.com/yichozy/hopebox/req"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
)

// ErrFigureNotFound covers every miss in GetDocumentImage: unknown doc,
// no figure by that name, or the OSS object is gone. Callers map it to
// a null/not-found response rather than an internal error.
var ErrFigureNotFound = errors.New("figure not found")

// GetDocumentImage returns a figure (page/caption + base64 image bytes)
// for a document. The pipeline persists images to OSS at
// passion-index/<docID>/images/<name> (see generate_document_tree.go);
// Figure.Data is hydrated on demand here — the models comment promised
// this, nothing fulfilled it until now.
func GetDocumentImage(ctx context.Context, doc_id uuid.UUID, name string) (*models.Figure, error) {
	doc, err := orm_document.GetDocumentByID(ctx, doc_id)
	if err != nil {
		return nil, err
	}
	if doc.ID == uuid.Nil {
		return nil, ErrFigureNotFound
	}

	// page + caption live on the node rows' figures, not in a dedicated
	// table — scan the doc's rows for the name.
	rows, err := orm_node.GetByDocID(ctx, doc_id)
	if err != nil {
		return nil, err
	}
	var found *models.Figure
	for i := range rows {
		for j := range rows[i].Figures {
			if rows[i].Figures[j].Name == name {
				figure := rows[i].Figures[j]
				found = &figure
				break
			}
		}
		if found != nil {
			break
		}
	}
	if found == nil {
		return nil, ErrFigureNotFound
	}

	oss, err := aliyun.NewOss()
	if err != nil {
		return nil, fmt.Errorf("figure %s: init oss: %w", name, err)
	}
	// The pipeline uploads images under the raw basename returned by MinerU
	// (`generate_document_tree.go` uses filepath.Base(f.Name)`); escaping the
	// name here would change the OSS key and make legitimate figures miss.
	object_key := fmt.Sprintf("passion-index/%s/images/%s", doc_id, name)
	// The hopebox wrapper has no bytes-download; the signed URL is the
	// cheapest way to stream the object without a temp file.
	signed_url, err := oss.GetObjectURL(ctx, object_key)
	if err != nil {
		return nil, fmt.Errorf("figure %s: sign url: %w", name, err)
	}
	resp, err := hope_req.GetContent(ctx, signed_url, nil, nil)
	if err != nil {
		if strings.Contains(err.Error(), "unexpected status code") {
			return nil, fmt.Errorf("figure %s: %w", name, ErrFigureNotFound)
		}
		return nil, fmt.Errorf("figure %s: %w", name, err)
	}
	found.Data = base64.StdEncoding.EncodeToString(resp.ByteContent)
	return found, nil
}
