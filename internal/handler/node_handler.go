package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_node"
	"gorm.io/gorm"
)

// NodeHandler serves the node routes.
type NodeHandler struct{}

// Get: GET /getNodeById?id — one section (tree node) with its children.
func (h *NodeHandler) GetNodeById(c *gin.Context) {
	id, err := uuid.Parse(c.Query("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad node id"})
		return
	}
	node, err := orm_node.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "node not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, node)
}
