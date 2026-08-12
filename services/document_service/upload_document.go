package document_service

import (
	"context"
	"fmt"
	"io"

	"github.com/google/uuid"
	"github.com/yichozy/hopebox/aliyun"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/models"
)

// UploadDocument reads the PDF, uploads it to OSS, creates a PENDING row
// in DB, and kicks off background processing via GenerateDocumentTree.
// metadata is an optional free-form JSON object stored on the document.
func UploadDocument(ctx context.Context, reader io.Reader, filename string, folder_id *uuid.UUID, metadata map[string]any) (*models.Document, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return nil, fmt.Errorf("generate uuid: %w", err)
	}
	file_key := fmt.Sprintf("passion-index/%s/%s", id, filename)

	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read upload: %w", err)
	}

	oss, err := aliyun.NewOss()
	if err != nil {
		return nil, fmt.Errorf("init oss: %w", err)
	}
	if err := oss.UploadBytes(ctx, file_key, data); err != nil {
		return nil, fmt.Errorf("upload to oss: %w", err)
	}

	doc := &models.Document{
		BaseUUIDModel: models.BaseUUIDModel{ID: id},
		Filename:      filename,
		FileKey:       file_key,
		Status:        models.StatusPending,
		FolderID:      folder_id,
		Metadata:      metadata,
	}
	if err := orm_document.Create(ctx, doc); err != nil {
		return nil, fmt.Errorf("create doc row: %w", err)
	}

	go GenerateDocumentTree(context.WithoutCancel(ctx), id)

	return doc, nil
}
