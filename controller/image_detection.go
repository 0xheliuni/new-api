package controller

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// imageDetectionTargetedRequest triggers a synchronous targeted detection run
// against the given channel IDs. Only channels carrying OpenAI image-series
// models produce detection records; the admin selects them here.
type imageDetectionTargetedRequest struct {
	ChannelIDs []int `json:"channel_ids"`
	// Limit caps how many undetected tasks per channel to process.
	Limit int `json:"limit"`
}

// TriggerImageDetection enqueues a targeted image detection system task for
// the given channel IDs. Rejects when a task of the same type is already active.
func TriggerImageDetection(c *gin.Context) {
	var req imageDetectionTargetedRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		common.ApiError(c, err)
		return
	}
	if len(req.ChannelIDs) == 0 {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("channel_ids is required"), nil)
		return
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}

	payload := imageDetectionTaskPayload{
		ChannelIDs: req.ChannelIDs,
	}
	task, created, err := service.EnqueueSystemTask(model.SystemTaskTypeImageDetection, payload)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	if !created {
		msg := common.NewMessage("An image detection task is already running or queued. Cannot start this manual task")
		common.ApiErrorStatus(c, http.StatusConflict, msg, gin.H{"data": gin.H{
			"task_id": task.TaskID,
			"status":  task.Status,
			"type":    task.Type,
		}})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"task_id": task.TaskID,
			"status":  task.Status,
		},
	})
}

// GetImageDetectionRecords returns paginated detection records with optional
// channel and trigger-mode filters.
func GetImageDetectionRecords(c *gin.Context) {
	channelID, _ := strconv.Atoi(c.Query("channel_id"))
	triggerMode := c.Query("trigger_mode")
	page, _ := strconv.Atoi(c.Query("page"))
	if page <= 0 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.Query("page_size"))
	if pageSize <= 0 {
		pageSize = 20
	}

	records, total, err := model.GetImageDetectionRecords(channelID, triggerMode, page, pageSize)
	if err != nil {
		common.ApiError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data": gin.H{
			"records": records,
			"total":   total,
		},
	})
}

// retainedImageExtensions names the download for each image type a detection
// can keep; other types are served without an extension.
var retainedImageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// GetImageDetectionImage serves the image kept for a detection whose content
// provenance check could not finish, so an operator can check it by hand on
// the official OpenAI verify page. Root only; images are user content.
func GetImageDetectionImage(c *gin.Context) {
	requestID := strings.TrimSpace(c.Query("request_id"))
	if requestID == "" {
		common.ApiErrorStatus(c, http.StatusBadRequest, common.NewMessage("request_id is required"), nil)
		return
	}
	rec, err := model.GetRetainedDetectionImage(requestID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		common.ApiErrorStatus(c, http.StatusNotFound, common.NewMessage("The image is no longer available"), nil)
		return
	}
	if err != nil {
		common.ApiError(c, err)
		return
	}
	data, err := os.ReadFile(rec.TempFilePath)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusNotFound, common.NewMessage("The image is no longer available"), nil)
		return
	}
	mediaType := http.DetectContentType(data)
	c.Header("Content-Disposition",
		fmt.Sprintf(`attachment; filename="image-detection-%d%s"`, rec.ID, retainedImageExtensions[mediaType]))
	c.Header("Cache-Control", "no-store")
	c.Header("X-Content-Type-Options", "nosniff")
	c.Data(http.StatusOK, mediaType, data)
}

// c2paVersion is the only release we auto-install.
const c2paVersion = "v0.28.2"

// c2paReleaseBaseURL is the GitHub release download base URL.
const c2paReleaseBaseURL = "https://github.com/contentauth/c2patool/releases/download/" + c2paVersion

// c2paDefaultInstallDir is the directory written when the in-container install button is used.
// Using /usr/local/bin keeps the binary on PATH in standard Linux containers.
const c2paDefaultInstallDir = "/usr/local/bin"

// c2paExecutableName is the platform binary name inside the archive.
func c2paExecutableName() string {
	if runtime.GOOS == "windows" {
		return "c2patool.exe"
	}
	return "c2patool"
}

// c2paArchiveURL returns the release asset URL for the current platform/arch.
// Only linux/amd64, windows/amd64, and darwin (universal) are released by
// contentauth; other targets return an empty string.
func c2paArchiveURL() string {
	switch {
	case runtime.GOOS == "linux" && runtime.GOARCH == "amd64":
		return c2paReleaseBaseURL + "/" + c2paVersion + "-x86_64-unknown-linux-gnu.tar.gz"
	case runtime.GOOS == "windows" && runtime.GOARCH == "amd64":
		return c2paReleaseBaseURL + "/" + c2paVersion + "-x86_64-pc-windows-msvc.zip"
	case runtime.GOOS == "darwin":
		return c2paReleaseBaseURL + "/" + c2paVersion + "-universal-apple-darwin.zip"
	default:
		return ""
	}
}

// InstallC2PATool downloads the c2patool binary for the current platform,
// writes it to /usr/local/bin (Linux/macOS) or %TEMP% (Windows), and updates
// the image_detection_setting.c2pa_tool_path option in the database.
//
// POST /api/image-detection/install-c2pa
// Body: optional JSON { "install_dir": "/custom/path" }
// Response: { "success": true, "data": { "path": "..." } }
func InstallC2PATool(c *gin.Context) {
	var req struct {
		InstallDir string `json:"install_dir"`
	}
	// ignore bind error — empty body is fine
	_ = c.ShouldBindJSON(&req)

	archiveURL := c2paArchiveURL()
	if archiveURL == "" {
		common.ApiErrorStatus(c, http.StatusUnprocessableEntity,
			common.NewMessage("c2patool has no prebuilt binary for this platform ({{goos}}/{{goarch}})",
				map[string]any{"goos": runtime.GOOS, "goarch": runtime.GOARCH}), nil)
		return
	}

	installDir := strings.TrimSpace(req.InstallDir)
	if installDir == "" {
		if runtime.GOOS == "windows" {
			installDir = os.TempDir()
		} else {
			installDir = c2paDefaultInstallDir
		}
	}
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError,
			common.NewMessage("Failed to create install directory: {{err}}", map[string]any{"err": err.Error()}), nil)
		return
	}

	// Download archive.
	resp, err := service.GetHttpClient().Get(archiveURL)
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadGateway,
			common.NewMessage("Failed to download c2patool: {{err}}", map[string]any{"err": err.Error()}), nil)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		common.ApiErrorStatus(c, http.StatusBadGateway,
			common.NewMessage("c2patool download returned status {{status}}", map[string]any{"status": resp.StatusCode}), nil)
		return
	}

	archiveData, err := io.ReadAll(io.LimitReader(resp.Body, 100*1024*1024))
	if err != nil {
		common.ApiErrorStatus(c, http.StatusBadGateway,
			common.NewMessage("Failed to read c2patool archive: {{err}}", map[string]any{"err": err.Error()}), nil)
		return
	}

	// Extract binary from archive.
	exeName := c2paExecutableName()
	// Archive has a c2patool/ directory prefix; the binary lives at c2patool/<name>.
	archiveBinaryPath := "c2patool/" + exeName

	var binData []byte
	if strings.HasSuffix(archiveURL, ".tar.gz") {
		binData, err = extractFromTarGz(archiveData, archiveBinaryPath)
	} else {
		binData, err = extractFromZip(archiveData, archiveBinaryPath)
	}
	if err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError,
			common.NewMessage("Failed to extract c2patool binary: {{err}}", map[string]any{"err": err.Error()}), nil)
		return
	}

	// Write binary to install directory.
	destPath := filepath.Join(installDir, exeName)
	if err = os.WriteFile(destPath, binData, 0o755); err != nil {
		common.ApiErrorStatus(c, http.StatusInternalServerError,
			common.NewMessage("Failed to write c2patool binary: {{err}}", map[string]any{"err": err.Error()}), nil)
		return
	}

	// Persist the path into the image detection setting.
	if err = model.UpdateOption("image_detection_setting.c2pa_tool_path", destPath); err != nil {
		// Non-fatal: binary is installed; admin can save the path manually.
		common.SysLog(fmt.Sprintf("c2patool installed to %s but option update failed: %v", destPath, err))
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "",
		"data":    gin.H{"path": destPath},
	})
}

// extractFromTarGz extracts the file at targetPath from a .tar.gz archive.
func extractFromTarGz(data []byte, targetPath string) ([]byte, error) {
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == targetPath {
			return io.ReadAll(io.LimitReader(tr, 100*1024*1024))
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", targetPath)
}

// extractFromZip extracts the file at targetPath from a .zip archive.
func extractFromZip(data []byte, targetPath string) ([]byte, error) {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	for _, f := range r.File {
		if f.Name == targetPath {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(io.LimitReader(rc, 100*1024*1024))
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", targetPath)
}
