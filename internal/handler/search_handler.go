package handler

// SearchHandler serves the search routes. Doc-level search dispatches
// on mode (semantic = vector recall + DocScore, keyword = BM25); node
// search is BM25 over node content. The metadata JSONB filter param
// was dropped — no consumer ever passed it.

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_document"
	"github.com/yichozy/passion-index/internal/orm_node"
	"github.com/yichozy/passion-index/models"
	"github.com/yichozy/passion-index/services/document_search_service"
)

// SearchHandler serves the search routes.
type SearchHandler struct{}

// Documents: GET /searchDocuments?q&mode=semantic|keyword&folder_id&recursive&limit
func (h *SearchHandler) SearchDocuments(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q is required"})
		return
	}
	mode := document_search_service.SEARCH_MODE_SEMANTIC
	if c.Query("mode") == "keyword" {
		mode = document_search_service.SEARCH_MODE_KEYWORD
	}
	var folder_id *uuid.UUID
	if raw := c.Query("folder_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad folder_id"})
			return
		}
		folder_id = &id
	}
	rows, err := document_search_service.SearchDocuments(c.Request.Context(), query, folder_id,
		c.Query("recursive") == "true", nil, query_int(c, "limit", 10), mode)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []orm_document.DocumentWithScore{}
	}
	c.JSON(http.StatusOK, rows)
}

// Nodes: GET /searchNodes?q&folder_id(required)&recursive&limit —
// BM25 over node title+summary+text.
func (h *SearchHandler) SearchNodes(c *gin.Context) {
	query := c.Query("q")
	if query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "q is required"})
		return
	}
	folder_id, err := uuid.Parse(c.Query("folder_id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "folder_id is required"})
		return
	}
	rows, err := orm_node.SearchNodesByBm25(c.Request.Context(), query, folder_id,
		c.Query("recursive") == "true", nil, query_int(c, "limit", 20))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if rows == nil {
		rows = []models.NodeWithScore{}
	}
	c.JSON(http.StatusOK, rows)
}
