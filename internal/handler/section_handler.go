package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yichozy/passion-index/services/document_service"
)

// SectionHandler serves the document-section (tree node) routes.
type SectionHandler struct{}

// GetDocumentSectionById: GET /getDocumentSectionById?id — one section
// (tree node) with its direct children (attached in
// document_service.GetSection).
func (h *SectionHandler) GetDocumentSectionById(c *gin.Context) {
	id, err := uuid.Parse(c.Query("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad section id"})
		return
	}
	node, err := document_service.GetSection(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, document_service.ErrSectionNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, node)
}
