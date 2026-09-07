package handler

import (
	"io"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"

	"github.com/theanhtong/rag_system/internal/ingest"
)

// DocumentHandler manages document ingestion HTTP endpoints.
type DocumentHandler struct {
	pipeline ingest.Pipeline
}

// NewDocumentHandler constructs a new DocumentHandler instance.
func NewDocumentHandler(p ingest.Pipeline) *DocumentHandler {
	return &DocumentHandler{pipeline: p}
}

// IngestTextRequest defines payload for JSON-based document text ingestion.
type IngestTextRequest struct {
	DocumentID   string `json:"document_id"`
	Filename     string `json:"filename"`
	Content      string `json:"content"`
	ChunkSize    int    `json:"chunk_size"`
	ChunkOverlap int    `json:"chunk_overlap"`
}

// HandleIngest handles POST /v1/documents/ingest (supports JSON payload & multipart file upload).
func (h *DocumentHandler) HandleIngest(c *gin.Context) {
	var filename string
	var content string
	var docID string
	chunkSize := 500
	chunkOverlap := 50

	contentType := c.ContentType()
	if contentType == "application/json" {
		var req IngestTextRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code":    "invalid_payload",
					"message": "failed to parse json request payload",
				},
			})
			return
		}
		filename = req.Filename
		content = req.Content
		docID = req.DocumentID
		if req.ChunkSize > 0 {
			chunkSize = req.ChunkSize
		}
		if req.ChunkOverlap > 0 {
			chunkOverlap = req.ChunkOverlap
		}
	} else {
		// handle multipart form file upload
		fileHeader, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{
				"error": gin.H{
					"code":    "missing_file",
					"message": "multipart form key 'file' is required",
				},
			})
			return
		}

		file, err := fileHeader.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"code":    "file_open_failed",
					"message": "failed to read uploaded file stream",
				},
			})
			return
		}
		defer file.Close()

		fileBytes, err := io.ReadAll(file)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{
				"error": gin.H{
					"code":    "file_read_failed",
					"message": "failed to read file content",
				},
			})
			return
		}

		filename = filepath.Base(fileHeader.Filename)
		content = string(fileBytes)
		docID = c.PostForm("document_id")
	}

	if len(content) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": gin.H{
				"code":    "empty_content",
				"message": "document content cannot be empty",
			},
		})
		return
	}

	if filename == "" {
		filename = "uploaded_doc.txt"
	}

	cfg := ingest.ChunkingConfig{
		ChunkSize:    chunkSize,
		ChunkOverlap: chunkOverlap,
	}

	result, err := h.pipeline.IngestDocument(c.Request.Context(), docID, filename, content, cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"code":    "ingestion_failed",
				"message": err.Error(),
			},
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   result,
	})
}

// HandleListDocuments handles GET /v1/documents.
func (h *DocumentHandler) HandleListDocuments(c *gin.Context) {
	docs := h.pipeline.ListDocuments()
	c.JSON(http.StatusOK, gin.H{
		"status": "success",
		"data":   docs,
	})
}
