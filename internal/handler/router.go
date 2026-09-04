package handler

// Register wires every REST route onto the gin engine with handler
// instances. Verb-noun paths (getDocumentList / uploadDocument …) with
// query params on GETs and JSON bodies on writes; HTTP verbs keep their
// semantics (DELETE for deletes, PATCH for renames). POST /chat is
// reserved for the document-QA chat feature.

import (
	"github.com/gin-gonic/gin"
)

func Register(r *gin.Engine) {
	documents := &DocumentHandler{}
	folders := &FolderHandler{}
	nodes := &NodeHandler{}
	search := &SearchHandler{}

	r.GET("/getDocumentList", documents.GetDocumentList)
	r.POST("/uploadDocument", documents.UploadDocument)
	r.GET("/searchDocuments", search.SearchDocuments)
	r.GET("/getDocumentById", documents.GetDocumentById)
	r.DELETE("/deleteDocument", documents.DeleteDocument)
	r.POST("/resummarizeDocument", documents.ResummarizeDocument)
	r.GET("/getDocumentSections", documents.GetDocumentSections)
	r.GET("/getFigure", documents.GetFigure)
	r.GET("/documents/:id/images/:name", documents.GetImageFile) // 302 direct link for browsers / LLM providers

	r.GET("/searchNodes", search.SearchNodes)
	r.GET("/getNodeById", nodes.GetNodeById)

	r.GET("/getFolderTree", folders.GetFolderTree)
	r.GET("/getFolderById", folders.GetFolderById)
	r.POST("/createFolder", folders.CreateFolder)
	r.PATCH("/renameFolder", folders.RenameFolder)
	r.DELETE("/deleteFolder", folders.DeleteFolder)
}
