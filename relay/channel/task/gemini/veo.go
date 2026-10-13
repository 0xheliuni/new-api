package gemini

import (
	"encoding/base64"
	"io"
	"strings"

	"github.com/QuantumNous/new-api/common"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// VeoImage represents an image input for Veo requests.
type VeoImage struct {
	BytesBase64Encoded string `json:"bytesBase64Encoded,omitempty"`
	MimeType           string `json:"mimeType,omitempty"`
	GcsURI             string `json:"gcsUri,omitempty"`
}

// VeoInstance is the per-instance payload sent to the Veo predictLongRunning endpoint.
type VeoInstance struct {
	Prompt string    `json:"prompt"`
	Image  *VeoImage `json:"image,omitempty"`
}

// VeoParameters holds generation parameters for Veo requests.
type VeoParameters struct {
	AspectRatio     string `json:"aspectRatio,omitempty"`
	DurationSeconds int    `json:"durationSeconds,omitempty"`
	Resolution      string `json:"resolution,omitempty"`
	SampleCount     int    `json:"sampleCount,omitempty"`
}

// VeoRequestPayload is the top-level request body for Veo predictLongRunning.
type VeoRequestPayload struct {
	Instances  []VeoInstance  `json:"instances"`
	Parameters *VeoParameters `json:"parameters,omitempty"`
}

// submitResponse is the response body returned by predictLongRunning.
type submitResponse struct {
	Name string `json:"name"`
}

// operationResponse is the response body returned by the Gemini operations poll endpoint.
type operationResponse struct {
	Name  string `json:"name"`
	Done  bool   `json:"done"`
	Error struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error"`
	Response struct {
		GenerateVideoResponse struct {
			GeneratedVideos []struct {
				Video struct {
					URI string `json:"uri"`
				} `json:"video"`
			} `json:"generatedSamples"`
			RAIMediaFilteredCount int `json:"raiMediaFilteredCount"`
		} `json:"generateVideoResponse"`
	} `json:"response"`
}

// ResolveVeoDuration extracts a duration in seconds from metadata or the request fields.
func ResolveVeoDuration(metadata map[string]any, duration int, seconds string) int {
	if d, ok := metadata["duration_seconds"]; ok {
		if v, ok := d.(float64); ok && v > 0 {
			return int(v)
		}
	}
	if duration > 0 {
		return duration
	}
	if seconds != "" {
		for i := 0; i < len(seconds); i++ {
			if seconds[i] < '0' || seconds[i] > '9' {
				break
			}
			if i == len(seconds)-1 {
				n := 0
				for _, c := range seconds {
					if c >= '0' && c <= '9' {
						n = n*10 + int(c-'0')
					}
				}
				return n
			}
		}
	}
	return 5 // default to 5 seconds
}

// ResolveVeoResolution extracts a resolution from metadata or the size string.
func ResolveVeoResolution(metadata map[string]any, size string) string {
	if r, ok := metadata["resolution"]; ok {
		if v, ok := r.(string); ok && v != "" {
			return v
		}
	}
	return SizeToVeoResolution(size)
}

// VeoResolutionRatio returns a billing multiplier for resolution.
func VeoResolutionRatio(model, resolution string) float64 {
	switch strings.ToLower(resolution) {
	case "1080p":
		return 2.0
	case "720p":
		return 1.5
	default:
		return 1.0
	}
}

// SizeToVeoResolution maps a size string (e.g. "1280x720") to a Veo resolution string.
func SizeToVeoResolution(size string) string {
	size = strings.ToLower(strings.ReplaceAll(size, " ", ""))
	switch size {
	case "1920x1080", "1920*1080":
		return "1080p"
	case "1280x720", "1280*720":
		return "720p"
	case "854x480", "854*480":
		return "480p"
	default:
		return ""
	}
}

// SizeToVeoAspectRatio maps a size string to a Veo aspect ratio string.
func SizeToVeoAspectRatio(size string) string {
	size = strings.ToLower(strings.ReplaceAll(size, " ", ""))
	switch {
	case strings.Contains(size, "16:9") || size == "1920x1080" || size == "1280x720" || size == "1920*1080" || size == "1280*720":
		return "16:9"
	case strings.Contains(size, "9:16"):
		return "9:16"
	case strings.Contains(size, "1:1"):
		return "1:1"
	default:
		return ""
	}
}

// ExtractMultipartImage extracts an uploaded image from a multipart request and
// returns a VeoImage if one is found.
func ExtractMultipartImage(c *gin.Context, info *relaycommon.RelayInfo) *VeoImage {
	if c == nil || c.Request == nil {
		return nil
	}
	form, err := common.ParseMultipartFormReusable(c)
	if err != nil || form == nil {
		return nil
	}
	for _, files := range form.File {
		if len(files) == 0 {
			continue
		}
		f, err := files[0].Open()
		if err != nil {
			continue
		}
		data, readErr := io.ReadAll(f)
		f.Close()
		if readErr != nil {
			continue
		}
		encoded := base64.StdEncoding.EncodeToString(data)
		mime := files[0].Header.Get("Content-Type")
		if mime == "" {
			mime = "image/jpeg"
		}
		return &VeoImage{BytesBase64Encoded: encoded, MimeType: mime}
	}
	return nil
}

// ParseImageInput parses a URL or data-URI image string into a VeoImage.
func ParseImageInput(input string) *VeoImage {
	if strings.HasPrefix(input, "gs://") {
		return &VeoImage{GcsURI: input}
	}
	if strings.HasPrefix(input, "data:") {
		parts := strings.SplitN(input, ",", 2)
		if len(parts) != 2 {
			return nil
		}
		meta := parts[0] // e.g. "data:image/png;base64"
		b64 := parts[1]
		mime := ""
		if idx := strings.Index(meta, ":"); idx >= 0 {
			mime = strings.TrimSuffix(meta[idx+1:], ";base64")
		}
		return &VeoImage{BytesBase64Encoded: b64, MimeType: mime}
	}
	if strings.HasPrefix(input, "http://") || strings.HasPrefix(input, "https://") {
		return &VeoImage{GcsURI: input}
	}
	return nil
}
