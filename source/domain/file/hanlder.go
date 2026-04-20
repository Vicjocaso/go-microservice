package file

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"file-manager/auth"
	"file-manager/telemetry"
	"file-manager/utils"

	"github.com/labstack/echo/v4"
)

type FileHandler struct {
	fileRepo *FileRepo
}

// NewFileHandler creates a new FileHandler instance.
func NewFileHandler(fr *FileRepo) *FileHandler {
	return &FileHandler{fileRepo: fr}
}

// NewPreviewHandler returns a handler that only supports PreviewTemplate (Gotenberg PDF preview).
// It does not require cloud storage; do not use other methods on this handler.
func NewPreviewHandler() *FileHandler {
	return &FileHandler{fileRepo: nil}
}

// UploadFile handles file upload via POST request.
func (h *FileHandler) UploadFile(c echo.Context) error {
	// serverID, err := auth.GetServerIDFromContext(c)
	// if err != nil {
	// 	return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required: Server ID not found in context")
	// }

	file, err := c.FormFile("file")
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, fmt.Sprintf("Failed to get file from form: %v", err))
	}

	logicalPath := c.FormValue("logical_path")
	if logicalPath == "" {
		logicalPath = "/" + file.Filename // Default logical path
	}

	targetCloud := c.FormValue("target_cloud") // Optional: specify a single cloud for upload

	fileMeta, err := h.fileRepo.UploadFile(c.Request().Context(), file, logicalPath, targetCloud)
	if err != nil {
		log.Printf("Error uploading file: %v", err)
		return echo.NewHTTPError(http.StatusInternalServerError, fmt.Sprintf("Failed to upload file: %v", err))
	}

	return c.JSON(http.StatusCreated, fileMeta)
}

// GetFileMetadata handles retrieving file metadata via GET request.
func (h *FileHandler) GetFileMetadata(c echo.Context) error {
	serverID, err := auth.GetServerIDFromContext(c)
	if err != nil {
		return echo.NewHTTPError(http.StatusUnauthorized, "Authentication required: Server ID not found in context")
	}

	fileID := c.Param("id")
	if fileID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "File ID is required")
	}

	fileMeta, err := h.fileRepo.GetFileMetadata(c.Request().Context(), fileID)
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "File metadata not found")
	}

	// Authorization check
	serverRole, ok := c.Get("serverRole").(string)
	if !ok || (serverRole != "admin" && serverID != fileMeta.UploadedBy) {
		return echo.NewHTTPError(http.StatusForbidden, "Access denied. You are not authorized to view this file's metadata.")
	}

	return c.JSON(http.StatusOK, fileMeta)
}

// Handler to render HTML template from uploaded file and data and then upload to the s3 bucket
func (fh *FileHandler) Insert(c echo.Context) error {
	logger := telemetry.SLogger(c.Request().Context())

	// Parse the multipart form
	err := c.Request().ParseMultipartForm(10 << 20)
	if err != nil {
		errMsg := map[string]string{"Error": "Unable to parse form"}
		logger.Error("Could not parse form", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}

	// Get the template file from the form
	templateFile, _, err := c.Request().FormFile("template")
	if err != nil {
		errMsg := map[string]string{"Error": "Error retrieving template file"}
		logger.Error("Could not retrieve template file", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}

	// Close the template file
	defer templateFile.Close()

	// Read the template file
	templateBytes, err := io.ReadAll(templateFile)
	if err != nil {
		errMsg := map[string]string{"Error": "Error reading template file"}
		logger.Error("Could not read template file", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}

	// Get the JSON data from the form
	jsonData := c.FormValue("jsonData")
	if jsonData == "" {
		errMsg := map[string]string{"Error": "jsonData field is required"}
		logger.Error("jsonData field is required", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}

	// Parse the JSON data into a map
	var templateData map[string]interface{}
	err = json.Unmarshal([]byte(jsonData), &templateData)
	if err != nil {
		errMsg := map[string]string{"Error": fmt.Sprintf("Failed to parse jsonData: %v", err)}
		logger.Error("Failed to parse jsonData", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}

	templateData["BucketName"] = "name"

	//Parse the template
	formBuf, contentType, err := utils.ParseTemplate(templateBytes, templateData)
	if err != nil {
		errMsg := map[string]string{"Error": fmt.Sprintf("Failed to parse template: %v", err)}
		logger.Error("Failed to parse template", errMsg)
		return c.JSON(http.StatusInternalServerError, errMsg)
	}

	// Send to PDF conversion service goteb
	pdfBuf, err := utils.ParseTemplateToPDF(formBuf, contentType)
	if err != nil {
		errMsg := map[string]string{"Error": fmt.Sprintf("Failed to convert to PDF: %v", err)}
		logger.Error("Failed to convert to PDF", errMsg)
		return c.JSON(http.StatusInternalServerError, errMsg)
	}

	// Generate a unique filename for the PDF
	pdfFilename := fmt.Sprintf("generated-pdf-%s.pdf", time.Now().Format("20060102-150405"))

	// Create a new S3 client
	s3Client := utils.NewS3Client("test-file-manager-2025")

	// Upload the PDF to S3
	err = s3Client.UploadObject(pdfFilename, &pdfBuf)
	if err != nil {
		errMsg := map[string]string{"Error": fmt.Sprintf("Failed to upload PDF to S3: %v", err)}
		logger.Error("Failed to upload PDF to S3", errMsg)
		return c.JSON(http.StatusInternalServerError, errMsg)
	}

	// Create the user message as JSON
	userMessage := map[string]string{
		"message": "File has been successfully created and uploaded to S3.",
	}

	logger.Info("File has been successfully created and uploaded to S3", userMessage)

	return c.JSON(http.StatusOK, userMessage)
}

// Handler to render HTML template and return PDF directly (preview)
func (fh *FileHandler) PreviewTemplate(c echo.Context) error {
	logger := telemetry.SLogger(c.Request().Context())

	// Parse the multipart form
	err := c.Request().ParseMultipartForm(10 << 20)
	if err != nil {
		errMsg := map[string]string{"Error": "Unable to parse form"}
		logger.Error("Could not parse form", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}

	// Get the template file from the form
	templateFile, _, err := c.Request().FormFile("template")
	if err != nil {
		errMsg := map[string]string{"Error": "Error retrieving template file"}
		logger.Error("Could not retrieve template file", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}
	defer templateFile.Close()

	// Read the template file
	templateBytes, err := io.ReadAll(templateFile)
	if err != nil {
		errMsg := map[string]string{"Error": "Error reading template file"}
		logger.Error("Could not read template file", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}

	// Get the JSON data from the form
	jsonData := c.FormValue("jsonData")
	if jsonData == "" {
		errMsg := map[string]string{"Error": "jsonData field is required"}
		logger.Error("jsonData field is required", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}

	// Parse the JSON data into a map
	var templateData map[string]interface{}
	err = json.Unmarshal([]byte(jsonData), &templateData)
	if err != nil {
		errMsg := map[string]string{"Error": fmt.Sprintf("Failed to parse jsonData: %v", err)}
		logger.Error("Failed to parse jsonData", errMsg)
		return c.JSON(http.StatusBadRequest, errMsg)
	}

	templateData["BucketName"] = "name"

	//Parse the template
	formBuf, contentType, err := utils.ParseTemplate(templateBytes, templateData)
	if err != nil {
		errMsg := map[string]string{"Error": fmt.Sprintf("Failed to parse template: %v", err)}
		logger.Error("Failed to parse template", errMsg)
		return c.JSON(http.StatusInternalServerError, errMsg)
	}

	// Send to PDF conversion service goteb
	pdfBuf, err := utils.ParseTemplateToPDF(formBuf, contentType)
	if err != nil {
		errMsg := map[string]string{"Error": fmt.Sprintf("Failed to convert to PDF: %v", err)}
		logger.Error("Failed to convert to PDF", errMsg)
		return c.JSON(http.StatusInternalServerError, errMsg)
	}

	// Generate a unique filename for the PDF download header
	pdfFilename := fmt.Sprintf("preview-%s.pdf", time.Now().Format("20060102-150405"))
	c.Response().Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%s", pdfFilename))

	return c.Blob(http.StatusOK, "application/pdf", pdfBuf.Bytes())
}
