package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yichozy/passion-index/internal/orm_folder"
	"github.com/yichozy/passion-index/models"
	"github.com/yichozy/passion-index/services/folder_service"
)

// FolderHandler serves the folder routes: tree / get / create / rename /
// delete.
type FolderHandler struct{}

// Tree: GET /getFolderTree?folder_id&depth — nested tree with batched
// counts; folder_id omitted = top-level forest.
func (h *FolderHandler) GetFolderTree(c *gin.Context) {
	var folder_id *uuid.UUID
	if raw := c.Query("folder_id"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "bad folder_id"})
			return
		}
		folder_id = &id
	}
	roots, err := folder_service.GetFolderTree(c.Request.Context(), folder_id, query_int(c, "depth", 3))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if roots == nil {
		roots = []*models.FolderNode{}
	}
	c.JSON(http.StatusOK, roots)
}

// Get: GET /getFolderById?id — thin metadata.
func (h *FolderHandler) GetFolderById(c *gin.Context) {
	id, err := uuid.Parse(c.Query("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad folder id"})
		return
	}
	folder, err := orm_folder.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if folder == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "folder not found"})
		return
	}
	c.JSON(http.StatusOK, folder)
}

// Create: POST /createFolder {name, parent_id?}.
func (h *FolderHandler) CreateFolder(c *gin.Context) {
	var body struct {
		Name     string     `json:"name" binding:"required"`
		ParentID *uuid.UUID `json:"parent_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	folder := &models.Folder{Name: body.Name, ParentID: body.ParentID}
	if err := orm_folder.Create(c.Request.Context(), folder); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, folder)
}

// Rename: PATCH /renameFolder {id, name} — load → mutate → save.
func (h *FolderHandler) RenameFolder(c *gin.Context) {
	var body struct {
		ID   uuid.UUID `json:"id" binding:"required"`
		Name string    `json:"name" binding:"required"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "id and name are required"})
		return
	}
	id := body.ID
	folder, err := orm_folder.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if folder == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "folder not found"})
		return
	}
	folder.Name = body.Name
	if err := orm_folder.Update(c.Request.Context(), folder); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, folder)
}

// Delete: DELETE /deleteFolder?id — refuses (409) when documents still live
// in the folder.
func (h *FolderHandler) DeleteFolder(c *gin.Context) {
	id, err := uuid.Parse(c.Query("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad folder id"})
		return
	}
	if err := folder_service.DeleteFolder(c.Request.Context(), id); err != nil {
		if errors.Is(err, folder_service.ErrFolderNotEmpty) {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.Status(http.StatusNoContent)
}
