package model

import (
	"github.com/QuantumNous/new-api/common"

	"gorm.io/gorm"
)

// ImageDetectionSignal values for C2PA and SynthID result fields. "detected"
// means an AI provenance signal was found; "not_detected" means no supported
// signal was found (absence of evidence, NOT proof of human authorship per the
// OpenAI provenance API). "none" is the pre-run default for C2PA.
const (
	ImageDetectionSignalNone         = "none"
	ImageDetectionSignalDetected     = "detected"
	ImageDetectionSignalNotDetected  = "not_detected"
	ImageDetectionSignalInconclusive = "inconclusive"
	ImageDetectionSignalPending      = "pending" // L3 deferred, image retained on disk
	ImageDetectionSignalSkipped      = "skipped" // upstream layer already gave an answer
	ImageDetectionSignalError        = "error"
	ImageDetectionSignalNotChecked   = "not_checked"
)

// Reason codes stored with a layer that ended in "error". The usage-log
// details dialog translates them into the detection log.
const (
	ImageDetectionErrorToolUnavailable  = "tool_unavailable"  // c2patool missing or not executable
	ImageDetectionErrorTimeout          = "timeout"           // c2patool or the API did not answer in time
	ImageDetectionErrorFailed           = "failed"            // the check ran but could not produce a result
	ImageDetectionErrorNoAPIKey         = "no_api_key"        // no OpenAI key in settings or environment
	ImageDetectionErrorUnsupportedImage = "unsupported_image" // not PNG, JPEG or WebP
	ImageDetectionErrorRequestFailed    = "request_failed"    // the API could not be reached
	ImageDetectionErrorHTTP             = "http_error"        // the API answered with a non-200 status
	ImageDetectionErrorImageLost        = "image_lost"        // the retained image disappeared
	ImageDetectionErrorInconclusive     = "inconclusive"      // the API answered without a usable result
	ImageDetectionErrorInvalidAPIKey    = "invalid_api_key"   // the API rejected the key (401)
	ImageDetectionErrorNoAccess         = "no_access"         // the organization has no Content Provenance access (404)
	ImageDetectionErrorRejected         = "rejected"          // the API rejected the image (400)
	ImageDetectionErrorTooLarge         = "too_large"         // over the API's 50 MiB upload limit
	ImageDetectionErrorServerError      = "server_error"      // the API kept answering with a 5xx status
	// ImageDetectionErrorRateLimited goes with a layer that is still pending:
	// the API asked to slow down (429) and a later run checks the image.
	ImageDetectionErrorRateLimited = "rate_limited"
)

// Why the C2PA layer found no OpenAI credentials, stored in c2pa_error next to
// a "none" signal; an image without any manifest has no reason. A manifest
// only counts when it is issued by OpenAI, records an AI generation action and
// validates as trusted, the rule the Content Provenance API applies to C2PA.
const (
	ImageDetectionReasonInvalidCredentials   = "invalid_credentials"   // the image no longer matches its signed manifest
	ImageDetectionReasonOtherIssuer          = "other_issuer"          // the manifest is signed by someone other than OpenAI
	ImageDetectionReasonNoAIAction           = "no_ai_action"          // the manifest records no AI generation
	ImageDetectionReasonUntrustedCredentials = "untrusted_credentials" // the signature does not chain to a trusted certificate
)

// Detection status shown on the drawing log (绘图日志) for openai_image tasks.
// Stored inside the Task's Data JSON under the "detection_status" key.
const (
	TaskDetectionStatusNotChecked = "not_checked" // detection did not run
	TaskDetectionStatusChecking   = "checking"    // sampled, L3 pending
	TaskDetectionStatusChecked    = "checked"     // detection completed
	TaskDetectionStatusError      = "error"       // detection errored
)

// ImageDetectionRecord stores the result of one detection run for a single
// sampled image. With passive retention the image bytes are written to a temp
// file (TempFilePath) when L3 is deferred; the worker reads and deletes the
// file after resolving SynthID. task_id links back to the openai_image Task.
// request_id mirrors the log row's request_id so detection status can be
// enriched onto usage-log API responses without a task JOIN.
type ImageDetectionRecord struct {
	ID          int64  `json:"id"           gorm:"primary_key;autoIncrement"`
	TaskID      string `json:"task_id"      gorm:"type:varchar(64);index"`
	RequestID   string `json:"request_id"   gorm:"type:varchar(64);index"`
	ChannelID   int    `json:"channel_id"   gorm:"index"`
	Model       string `json:"model"        gorm:"type:varchar(128)"`
	ContentHash string `json:"content_hash" gorm:"type:varchar(64);index"`
	ImageURL    string `json:"image_url"    gorm:"type:varchar(512)"`

	// L1 resolution
	RequestSize string `json:"request_size" gorm:"type:varchar(32)"`
	ActualSize  string `json:"actual_size"  gorm:"type:varchar(32)"`
	SizeMatch   *bool  `json:"size_match"`

	LatencyMs int64 `json:"latency_ms"`

	// DurationMs is the total detection work time in milliseconds: L1 + L2,
	// plus the L3 provenance call when it runs. Time spent queued for the
	// scheduled task is not counted.
	DurationMs int64 `json:"duration_ms" gorm:"default:0"`

	// L2 C2PA, L3 SynthID — one of the Signal* constants
	C2PASignal    string `json:"c2pa_signal"     gorm:"type:varchar(32)"`
	SynthIDSignal string `json:"synth_id_signal" gorm:"type:varchar(32)"`

	// When a layer ends in "error", C2PAError / ProvenanceError hold one of the
	// ImageDetectionError* reason codes; ProvenanceErrorDetail keeps the raw
	// detail (HTTP status and response excerpt, media type, network error).
	C2PAError             string `json:"c2pa_error"              gorm:"column:c2pa_error;type:varchar(32)"`
	ProvenanceError       string `json:"provenance_error"        gorm:"column:provenance_error;type:varchar(32)"`
	ProvenanceErrorDetail string `json:"provenance_error_detail" gorm:"column:provenance_error_detail;type:varchar(512)"`

	ProvenanceAnswer `gorm:"embedded"`

	// Trigger: "global" | "scheduled" | "targeted"
	TriggerMode string `json:"trigger_mode" gorm:"type:varchar(32);index"`

	// TempFilePath holds retained image bytes for deferred L3. Never serialized
	// to API responses. Cleared by the worker after L3 resolves.
	TempFilePath string `json:"-" gorm:"type:varchar(512)"`

	CreatedAt int64 `json:"created_at" gorm:"bigint;index"`
	UpdatedAt int64 `json:"updated_at" gorm:"bigint;index"`
}

// ProvenanceAnswer is the Content Provenance API's answer for one image, one
// field per signal: the C2PA outcome with its validation state and issuer, the
// SynthID outcome, and the model the API names. The two signals are read
// independently, since re-encoding strips C2PA but not SynthID. Empty until
// the API has answered.
type ProvenanceAnswer struct {
	C2PA      string `json:"provenance_c2pa"       gorm:"column:provenance_c2pa;type:varchar(32)"`
	C2PAState string `json:"provenance_c2pa_state" gorm:"column:provenance_c2pa_state;type:varchar(32)"`
	Issuer    string `json:"provenance_issuer"     gorm:"column:provenance_issuer;type:varchar(128)"`
	SynthID   string `json:"provenance_synthid"    gorm:"column:provenance_synthid;type:varchar(32)"`
	Model     string `json:"provenance_model"      gorm:"column:provenance_model;type:varchar(64)"`
}

// Columns returns the answer keyed by column name, for a partial update.
func (a ProvenanceAnswer) Columns() map[string]any {
	return map[string]any{
		"provenance_c2pa":       a.C2PA,
		"provenance_c2pa_state": a.C2PAState,
		"provenance_issuer":     a.Issuer,
		"provenance_synthid":    a.SynthID,
		"provenance_model":      a.Model,
	}
}

func (r *ImageDetectionRecord) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	if r.CreatedAt == 0 {
		r.CreatedAt = now
	}
	if r.UpdatedAt == 0 {
		r.UpdatedAt = now
	}
	return nil
}

func (r *ImageDetectionRecord) BeforeUpdate(_ *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

func InsertImageDetectionRecord(record *ImageDetectionRecord) error {
	return DB.Create(record).Error
}

func UpdateImageDetectionRecord(id int64, updates map[string]any) error {
	return DB.Model(&ImageDetectionRecord{}).Where("id = ?", id).Updates(updates).Error
}

// GetImageDetectionRecords returns paginated detection records with optional
// channel and trigger filters. page is 1-indexed.
func GetImageDetectionRecords(channelID int, triggerMode string, page, pageSize int) ([]*ImageDetectionRecord, int64, error) {
	var records []*ImageDetectionRecord
	var total int64

	query := DB.Model(&ImageDetectionRecord{})
	if channelID > 0 {
		query = query.Where("channel_id = ?", channelID)
	}
	if triggerMode != "" {
		query = query.Where("trigger_mode = ?", triggerMode)
	}
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if page <= 0 {
		page = 1
	}
	offset := (page - 1) * pageSize
	if err := query.Order("id DESC").Limit(pageSize).Offset(offset).Find(&records).Error; err != nil {
		return nil, 0, err
	}
	return records, total, nil
}

// GetDetectionStatusesByRequestIDs returns a map of request_id → detection
// status for the given request IDs. Used to enrich usage-log API responses.
// Only the most recent record per request_id is considered (highest id).
func GetDetectionStatusesByRequestIDs(requestIDs []string) (map[string]ImageDetectionLogResult, error) {
	if len(requestIDs) == 0 {
		return nil, nil
	}
	type row struct {
		RequestID     string `gorm:"column:request_id"`
		SynthIDSignal string `gorm:"column:synth_id_signal"`
		// GORM's naming strategy maps the C2PASignal field to c2_pa_signal.
		C2PASignal  string `gorm:"column:c2_pa_signal"`
		SizeMatch   *bool  `gorm:"column:size_match"`
		RequestSize string `gorm:"column:request_size"`
		ActualSize  string `gorm:"column:actual_size"`
		DurationMs  int64  `gorm:"column:duration_ms"`

		C2PAError             string `gorm:"column:c2pa_error"`
		ProvenanceError       string `gorm:"column:provenance_error"`
		ProvenanceErrorDetail string `gorm:"column:provenance_error_detail"`
		TempFilePath          string `gorm:"column:temp_file_path"`
		ProvenanceAnswer
	}
	var rows []row
	// Grab the latest record per request_id by selecting the one with max(id).
	err := DB.Model(&ImageDetectionRecord{}).
		Select("request_id, synth_id_signal, c2_pa_signal, size_match, request_size, actual_size, duration_ms, "+
			"c2pa_error, provenance_error, provenance_error_detail, temp_file_path, "+
			"provenance_c2pa, provenance_c2pa_state, provenance_issuer, provenance_synthid, provenance_model").
		Where("request_id IN ? AND id IN (?)",
			requestIDs,
			DB.Model(&ImageDetectionRecord{}).
				Select("MAX(id)").
				Where("request_id IN ?", requestIDs).
				Group("request_id"),
		).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	result := make(map[string]ImageDetectionLogResult, len(rows))
	for _, r := range rows {
		// Verdict: a resolution mismatch always fails. Once SynthID (the
		// official content check) finishes with a definite answer it decides,
		// so a forced SynthID run can overrule a C2PA manifest. While SynthID
		// is still pending the verdict waits; otherwise a C2PA signal passes.
		verdict := ""
		switch {
		case r.SizeMatch != nil && !*r.SizeMatch:
			verdict = ImageDetectionVerdictFail
		case r.SynthIDSignal == ImageDetectionSignalDetected:
			verdict = ImageDetectionVerdictPass
		case r.SynthIDSignal == ImageDetectionSignalNotDetected:
			verdict = ImageDetectionVerdictFail
		case r.SynthIDSignal == ImageDetectionSignalPending:
		case r.C2PASignal == ImageDetectionSignalDetected:
			verdict = ImageDetectionVerdictPass
		}
		status := ""
		// Derive a single status from the signal fields:
		//  - SynthIDSignal pending         → "checking"
		//  - Any signal "error"            → "error"
		//  - both signals "not_checked"    → "not_checked"
		//  - otherwise                     → "checked"
		switch {
		case verdict != "":
			status = TaskDetectionStatusChecked
		case r.SynthIDSignal == ImageDetectionSignalPending:
			status = TaskDetectionStatusChecking
		case r.SynthIDSignal == ImageDetectionSignalError || r.C2PASignal == ImageDetectionSignalError:
			status = TaskDetectionStatusError
		case r.SynthIDSignal == ImageDetectionSignalNotChecked && (r.C2PASignal == ImageDetectionSignalNotChecked || r.C2PASignal == ImageDetectionSignalNone):
			status = TaskDetectionStatusNotChecked
		default:
			status = TaskDetectionStatusChecked
		}
		result[r.RequestID] = ImageDetectionLogResult{
			Status:      status,
			Verdict:     verdict,
			Resolution:  resolutionStepResult(r.SizeMatch),
			Metadata:    c2paStepResult(r.C2PASignal),
			Content:     synthIDStepResult(r.SynthIDSignal),
			RequestSize: r.RequestSize,
			ActualSize:  r.ActualSize,
			DurationMs:  r.DurationMs,

			MetadataError:      r.C2PAError,
			ContentError:       r.ProvenanceError,
			ContentErrorDetail: r.ProvenanceErrorDetail,
			// Only whether an image is kept is exposed, never its path.
			ImageAvailable: r.TempFilePath != "" && r.SynthIDSignal != ImageDetectionSignalPending,
			Provenance:     r.ProvenanceAnswer,
		}
	}
	return result, nil
}

// Detection verdicts surfaced on the usage-log details dialog.
const (
	ImageDetectionVerdictPass = "pass"
	ImageDetectionVerdictFail = "fail"
)

// Per-layer step results shown as the resolution → metadata → content
// pipeline on the usage-log details dialog.
const (
	ImageDetectionStepPass       = "pass"
	ImageDetectionStepFail       = "fail"
	ImageDetectionStepPending    = "pending"
	ImageDetectionStepError      = "error"
	ImageDetectionStepSkipped    = "skipped"
	ImageDetectionStepNotChecked = "not_checked"
)

// ImageDetectionLogResult is the per-request detection summary attached to
// usage-log rows. Verdict is empty while the result is still undecided.
type ImageDetectionLogResult struct {
	Status      string
	Verdict     string
	Resolution  string
	Metadata    string
	Content     string
	RequestSize string
	ActualSize  string
	// DurationMs is the total detection work time in milliseconds.
	DurationMs int64
	// Failure reason codes (ImageDetectionError*, or ImageDetectionReason* for
	// metadata that did not count) for the metadata and content provenance
	// steps, the raw content provenance detail, and whether the image is still
	// kept for manual verification.
	MetadataError      string
	ContentError       string
	ContentErrorDetail string
	ImageAvailable     bool
	// Provenance is the per-signal answer of the Content Provenance API.
	Provenance ProvenanceAnswer
}

// resolutionStepResult maps L1. A nil SizeMatch means the check did not run.
func resolutionStepResult(sizeMatch *bool) string {
	if sizeMatch == nil {
		return ImageDetectionStepNotChecked
	}
	if *sizeMatch {
		return ImageDetectionStepPass
	}
	return ImageDetectionStepFail
}

// c2paStepResult maps L2. "none" means c2patool ran and found no manifest;
// a disabled check is stored as not_checked by the pipeline.
func c2paStepResult(signal string) string {
	switch signal {
	case ImageDetectionSignalDetected:
		return ImageDetectionStepPass
	case ImageDetectionSignalNone, ImageDetectionSignalNotDetected:
		return ImageDetectionStepFail
	case ImageDetectionSignalError:
		return ImageDetectionStepError
	default:
		return ImageDetectionStepNotChecked
	}
}

// synthIDStepResult maps L3. Skipped means L2 already found a signal.
func synthIDStepResult(signal string) string {
	switch signal {
	case ImageDetectionSignalDetected:
		return ImageDetectionStepPass
	case ImageDetectionSignalNotDetected:
		return ImageDetectionStepFail
	case ImageDetectionSignalPending:
		return ImageDetectionStepPending
	case ImageDetectionSignalError, ImageDetectionSignalInconclusive:
		return ImageDetectionStepError
	case ImageDetectionSignalSkipped:
		return ImageDetectionStepSkipped
	default:
		return ImageDetectionStepNotChecked
	}
}

// GetPendingSynthIDRecords returns up to limit records whose SynthID signal is
// still pending and that still have a retained temp file, ordered oldest-first.
// channelIDs, when non-empty, restricts to those channels (targeted mode).
func GetPendingSynthIDRecords(limit int, channelIDs []int) ([]*ImageDetectionRecord, error) {
	var records []*ImageDetectionRecord
	query := DB.Where("synth_id_signal = ? AND temp_file_path != ''", ImageDetectionSignalPending)
	if len(channelIDs) > 0 {
		query = query.Where("channel_id IN ?", channelIDs)
	}
	err := query.Order("id ASC").Limit(limit).Find(&records).Error
	return records, err
}

// GetRetainedDetectionImage returns the latest finished record for requestID
// that still keeps its image for manual verification.
func GetRetainedDetectionImage(requestID string) (*ImageDetectionRecord, error) {
	var records []*ImageDetectionRecord
	err := DB.Where("request_id = ? AND temp_file_path != '' AND synth_id_signal != ?",
		requestID, ImageDetectionSignalPending).
		Order("id DESC").Limit(1).Find(&records).Error
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	return records[0], nil
}

// GetExpiredRetainedImages returns finished records whose kept image is past
// the retention window: last updated before cutoff, or beyond the newest keep
// records. limit bounds each query so one prune stays cheap. A record can
// appear twice; deleting its image is idempotent.
func GetExpiredRetainedImages(cutoff int64, keep, limit int) ([]*ImageDetectionRecord, error) {
	var expired []*ImageDetectionRecord
	err := DB.Where("temp_file_path != '' AND synth_id_signal != ? AND updated_at < ?",
		ImageDetectionSignalPending, cutoff).
		Order("id ASC").Limit(limit).Find(&expired).Error
	if err != nil {
		return nil, err
	}
	var overflow []*ImageDetectionRecord
	err = DB.Where("temp_file_path != '' AND synth_id_signal != ?", ImageDetectionSignalPending).
		Order("id DESC").Offset(keep).Limit(limit).Find(&overflow).Error
	if err != nil {
		return nil, err
	}
	return append(expired, overflow...), nil
}

// SetTaskDetectionStatus merges a detection status (and optional summary) into
// the openai_image Task's Data JSON so the drawing log can show it. The Task's
// Data column already carries prompt/size/urls; this adds detection_status and
// detection_summary without disturbing the other keys.
func SetTaskDetectionStatus(taskID, status, summary string) error {
	if taskID == "" {
		return nil
	}
	var task Task
	if err := DB.Select("id", "data").Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return err
	}
	data := map[string]any{}
	if len(task.Data) > 0 {
		_ = common.Unmarshal(task.Data, &data)
	}
	data["detection_status"] = status
	if summary != "" {
		data["detection_summary"] = summary
	}
	merged, err := common.Marshal(data)
	if err != nil {
		return err
	}
	return DB.Model(&Task{}).Where("task_id = ?", taskID).Update("data", merged).Error
}
