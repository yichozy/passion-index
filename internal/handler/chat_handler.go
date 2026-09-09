package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/yichozy/passion-index/models"
	"github.com/yichozy/passion-index/services/chat_service"
)

// ChatHandler serves the document-QA chat route.
type ChatHandler struct{}

// Chat: POST /chat {messages: [{role, content}], folder_id?} —
// blocking; runs the retrieval tool loop server-side (tens of
// seconds). folder_id scopes the whole conversation.
func (h *ChatHandler) Chat(c *gin.Context) {
	var body struct {
		Messages []models.ChatMessage `json:"messages" binding:"required,min=1"`
		FolderID *uuid.UUID           `json:"folder_id"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "messages (non-empty) are required"})
		return
	}
	result, err := chat_service.Chat(c.Request.Context(), body.Messages, body.FolderID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, result)
}
