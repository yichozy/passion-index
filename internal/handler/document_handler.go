package handler

// DocumentHandler serves the document routes: get / list / upload /
// delete / resummarize / sections-by-pages / figure (base64) / image
// (302 signed URL). Handlers are thin — business rules live in
// services/, same split the GraphQL resolvers had.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yichozy/hopebox/aliyun"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_folder"
	"github.com/yichozy/passion-index/models"
	"github.com/yichozy/passion-index/services/document_service"
)

type DocumentHandler struct{}

// folder_brief is the {id, name} hint on document payloads —
// models.Document hides its Folder relation (json:"-").
type folder_brief struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

// document_response is a document plus the fields the model hides:
// the folder hint and (get only) the assembled tree.
type document_response struct {
	models.Document
	Folder *folder_brief `json:"folder,omitempty"`
	Tree   *models.Node  `json:"tree,omitempty"`
}

// Get: GET /getDocumentById?id — metadata + folder hint + full tree.
func (h *DocumentHandler) GetDocumentById(c *gin.Context) {
	id, err := uuid.Parse(c.Query("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad document id"})
		return
	}
	doc, err := orm_document.GetDocumentByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if doc.ID == uuid.Nil {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("document %s not found", id)})
		return
	}
	var tree *models.Node
	if t, err := document_service.GetDocumentStructure(c.Request.Context(), id); err == nil {
		tree = t
	} // not-found/not-ready here just omit the tree; metadata still serves
	resp := document_response{Document: doc, Tree: tree}
	if doc.FolderID != nil {
		if folder, err := orm_folder.GetByID(c.Request.Context(), *doc.FolderID); err == nil && folder != nil {
			resp.Folder = &folder_brief{ID: folder.ID, Name: folder.Name}
		}
	}
	c.JSON(http.StatusOK, resp)
}

// List: GET /getDocumentList?folder_id&recursive&limit&offset.
// folder_id omitted = whole library (virtual root).
func (h *DocumentHandler) GetDocumentList(c *gin.Context) {
	var folder_id *uuid.UUID
	if raw := c.Query("folder_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad folder_id"})
			return
		}
		folder_id = &id
	}
	docs, total, err := orm_document.ListDocumentsByFolder(c.Request.Context(), folder_id,
		c.Query("recursive") == "true", query_int(c, "limit", 20), query_int(c, "offset", 0))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	items := make([]document_response, len(docs))
	for i := range docs {
		items[i] = document_response{Document: docs[i]}
		if docs[i].FolderID != nil {
			if folder, err := orm_folder.GetByID(c.Request.Context(), *docs[i].FolderID); err == nil && folder != nil {
				items[i].Folder = &folder_brief{ID: folder.ID, Name: folder.Name}
			}
		}
	}
	c.JSON(http.StatusOK, gin.H{"items": items, "total": total})
}

// Upload: POST /uploadDocument — multipart file + folder_id (+ metadata JSON).
func (h *DocumentHandler) UploadDocument(c *gin.Context) {
	folder_id, err := uuid.Parse(c.PostForm("folder_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "folder_id is required"})
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required"})
		return
	}
	reader, err := file.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unreadable file"})
		return
	}
	defer reader.Close()

	var metadata map[string]any
	if raw := c.PostForm("metadata"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("bad metadata JSON: %v", err)})
			return
		}
	}
	doc, err := document_service.UploadDocument(c.Request.Context(), reader, file.Filename, &folder_id, metadata)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp := document_response{Document: *doc}
	if doc.FolderID != nil {
		if folder, err := orm_folder.GetByID(c.Request.Context(), *doc.FolderID); err == nil && folder != nil {
			resp.Folder = &folder_brief{ID: folder.ID, Name: folder.Name}
		}
	}
	c.JSON(http.StatusCreated, resp)
}

// Delete: DELETE /deleteDocument?doc_id — soft-deletes doc + nodes atomically.
func (h *DocumentHandler) DeleteDocument(c *gin.Context) {
	id, err := uuid.Parse(c.Query("doc_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad document id"})
		return
	}
	deleted, err := orm_document.DeleteDocument(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !deleted {
		c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("document %s not found", id)})
		return
	}
	c.Status(http.StatusNoContent)
}

// Resummarize: POST /resummarizeDocument {doc_id, force} — async, 202.
func (h *DocumentHandler) ResummarizeDocument(c *gin.Context) {
	var body struct {
		DocID uuid.UUID `json:"doc_id" binding:"required"`
		Force bool      `json:"force"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "doc_id is required"})
		return
	}
	go document_service.ReSummarizeDocumentTree(context.WithoutCancel(c.Request.Context()), body.DocID, body.Force)
	c.JSON(http.StatusAccepted, gin.H{"status": "accepted"})
}

// GetPageContent: GET /getPageContent?doc_id&pages=5-10 — per-page
// markdown text (PageIndex get_page_content semantics); the spec is
// parsed and the read is gated in document_service.GetPageText.
func (h *DocumentHandler) GetPageContent(c *gin.Context) {
	id, err := uuid.Parse(c.Query("doc_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad document id"})
		return
	}
	pages, err := document_service.GetPageText(c.Request.Context(), id, c.Query("pages"))
	if err != nil {
		switch {
		case errors.Is(err, document_service.ErrBadPageSpec):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, document_service.ErrDocumentNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, document_service.ErrDocumentNotReady):
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		}
		return
	}
	c.JSON(http.StatusOK, pages)
}

// Figure: GET /getFigure?doc_id&name — base64 JSON, for
// programmatic/multimodal consumption (browsers and LLM providers that
// can fetch URLs use the 302 route instead).
func (h *DocumentHandler) GetFigure(c *gin.Context) {
	id, err := uuid.Parse(c.Query("doc_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad document id"})
		return
	}
	name := c.Query("name")
	figure, err := document_service.GetDocumentImage(c.Request.Context(), id, name)
	if err != nil {
		if errors.Is(err, document_service.ErrFigureNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("figure %q not found in document %s", name, id)})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, figure)
}

// Image redirects to a 24h-signed OSS URL. The client (browser /
// curl / LLM provider) fetches bytes directly from OSS; passion-index
// never proxies image bytes.
func (h *DocumentHandler) GetImageFile(c *gin.Context) {
	doc_id := c.Param("id")
	name := c.Param("name")
	// Reject path-traversal attempts; MinerU image names are plain basenames.
	if strings.Contains(name, "/") || strings.Contains(name, "\\") || strings.Contains(name, "..") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid image name"})
		return
	}
	oss, err := aliyun.NewOss()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "oss init failed"})
		return
	}
	url, err := oss.GetObjectURL(c.Request.Context(), fmt.Sprintf("passion-index/%s/images/%s", doc_id, name))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
		return
	}
	c.Redirect(http.StatusFound, url)
}

// query_int reads an int query param with a default (bad value → default).
func query_int(c *gin.Context, key string, fallback int) int {
	if raw := c.Query(key); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil {
			return v
		}
	}
	return fallback
}
