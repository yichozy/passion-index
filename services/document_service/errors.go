package document_service

import "errors"

// Read-path sentinels shared by the REST handlers and the chat tools.
// Handlers map them to HTTP statuses; chat tools wrap them into
// {"error": ...} tool results.
var (
	ErrDocumentNotFound = errors.New("document not found")
	ErrDocumentNotReady = errors.New("document not ready")
	ErrSectionNotFound  = errors.New("section not found")
	ErrBadPageSpec      = errors.New("bad pages spec")
)
