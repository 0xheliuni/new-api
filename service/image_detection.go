package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"io/fs"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// ImageDetectionTrigger identifies how a detection run was started.
const (
	ImageDetectionTriggerGlobal    = "global"
	ImageDetectionTriggerScheduled = "scheduled"
	ImageDetectionTriggerTargeted  = "targeted"
)

// provenanceAPIURL is the OpenAI Content Provenance API endpoint.
// POST /v1/content_provenance_checks, multipart/form-data, file field "file".
const provenanceAPIURL = "https://api.openai.com/v1/content_provenance_checks"

const synthIDResultCachePrefix = "imgdet:synthid:"
const synthIDRLMinutePrefix = "imgdet:rl:min:"
const synthIDRLDayPrefix = "imgdet:rl:day:"

// synthIDRequestTimeout bounds one provenance API call, upload included. The
// shared fetch client has no overall timeout unless RELAY_TIMEOUT is set, so
// without it a stalled connection would leave the record pending forever.
const synthIDRequestTimeout = 2 * time.Minute

// provenanceImageExtensions lists the image media types the Content
// Provenance API accepts, with the filename extension sent for each.
var provenanceImageExtensions = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
}

// provenanceMaxImageBytes is the Content Provenance API's upload limit.
const provenanceMaxImageBytes = 50 << 20

// provenanceMaxAttempts bounds the calls for one image when the API answers
// with a server error or cannot be reached; a timed-out call is not repeated.
const provenanceMaxAttempts = 3

// ImageDetectionJob carries everything needed to run the detection pipeline
// for a single image sampled from a relay response.
type ImageDetectionJob struct {
	TaskID      string
	RequestID   string // mirrors logs.request_id for usage-log enrichment
	ChannelID   int
	Model       string
	RequestSize string
	LatencyMs   int64
	// ImageBytes holds the raw PNG/JPEG bytes captured in-process. The relay
	// handler decodes b64 or reads URL before launching the goroutine so the
	// image is available even after the upstream URL expires.
	ImageBytes []byte
	Trigger    string
}

// RunImageDetectionPipeline executes L1 → L2 → L3 for one image.
// L1 (resolution) and L2 (C2PA) run synchronously; L3 (SynthID API) is
// deferred: the record is written as "pending" and the image is retained in
// a temp file until a worker calls ProcessPendingSynthIDRecords.
//
// Safe to call from gopool.Go; panics are caught by the pool's recover.
// Uses context.Background() so the pipeline outlives the HTTP request.
func RunImageDetectionPipeline(job ImageDetectionJob) {
	ctx := context.Background()
	s := operation_setting.GetImageDetectionSetting()
	start := time.Now()
	defer pruneRetainedDetectionImages(retainedImageMaxCount, retainedImageMaxAge)

	rec := &model.ImageDetectionRecord{
		TaskID:        job.TaskID,
		RequestID:     job.RequestID,
		ChannelID:     job.ChannelID,
		Model:         job.Model,
		RequestSize:   job.RequestSize,
		LatencyMs:     job.LatencyMs,
		TriggerMode:   job.Trigger,
		C2PASignal:    model.ImageDetectionSignalNone,
		SynthIDSignal: model.ImageDetectionSignalSkipped,
	}

	imgBytes := job.ImageBytes
	if len(imgBytes) == 0 {
		// Nothing to check — write a minimal record so the drawing log can
		// show "not_checked" rather than silence.
		rec.SynthIDSignal = model.ImageDetectionSignalNotChecked
		rec.C2PASignal = model.ImageDetectionSignalNotChecked
		_ = model.InsertImageDetectionRecord(rec)
		return
	}

	rec.ContentHash = imageContentHash(imgBytes)

	// L1: resolution check — always runs (when enabled), no sampling.
	if s.CheckResolution {
		actual, match := checkResolution(imgBytes, job.RequestSize)
		rec.ActualSize = actual
		rec.SizeMatch = &match
	}

	// L2: C2PA via c2patool — always runs (when enabled), no sampling.
	// A disabled check is recorded as not_checked so it is not mistaken for
	// "no manifest found" ("none").
	if s.CheckC2PA {
		rec.C2PASignal, rec.C2PAError = runC2PACheck(ctx, imgBytes,
			s.C2PAToolPath, time.Duration(s.C2PATimeoutSeconds)*time.Second)
	} else {
		rec.C2PASignal = model.ImageDetectionSignalNotChecked
	}

	// Short-circuit: L2 gave a definitive signal → skip L3, unless SynthID is
	// forced so the official content check runs regardless of the metadata.
	if s.CheckSynthID {
		definitive := rec.C2PASignal == model.ImageDetectionSignalDetected && !s.SynthIDForce
		if definitive {
			rec.SynthIDSignal = model.ImageDetectionSignalSkipped
			rec.DurationMs = time.Since(start).Milliseconds()
			_ = model.InsertImageDetectionRecord(rec)
			_ = model.SetTaskDetectionStatus(rec.TaskID, model.TaskDetectionStatusChecked,
				"c2pa:detected")
			return
		}

		// Check result cache before retaining the file.
		if cached, ok := synthIDResultFromCache(ctx, rec.ContentHash); ok {
			rec.SynthIDSignal = cached.Signal
			rec.ProvenanceAnswer = cached.ProvenanceAnswer
			rec.DurationMs = time.Since(start).Milliseconds()
			_ = model.InsertImageDetectionRecord(rec)
			status := model.TaskDetectionStatusChecked
			if cached.Signal == model.ImageDetectionSignalError {
				status = model.TaskDetectionStatusError
			}
			_ = model.SetTaskDetectionStatus(rec.TaskID, status, "synth_id:"+cached.Signal)
			return
		}

		// Retain image to a temp file for deferred L3 processing.
		tmpPath, err := retainImageToTempFile(imgBytes)
		if err != nil {
			logger.LogWarn(ctx, common.LogText("image detection: retain temp file failed task_id=%s: %v",
				job.TaskID, err))
			rec.SynthIDSignal = model.ImageDetectionSignalError
		} else {
			rec.TempFilePath = tmpPath
			rec.SynthIDSignal = model.ImageDetectionSignalPending
		}
	} else {
		rec.SynthIDSignal = model.ImageDetectionSignalNotChecked
	}

	rec.DurationMs = time.Since(start).Milliseconds()
	if err := model.InsertImageDetectionRecord(rec); err != nil {
		logger.LogWarn(ctx, common.LogText("image detection: insert record failed task_id=%s: %v",
			job.TaskID, err))
		return
	}
	if rec.SynthIDSignal == model.ImageDetectionSignalPending {
		_ = model.SetTaskDetectionStatus(rec.TaskID, model.TaskDetectionStatusChecking, "")
		// Resolve this record right away rather than the oldest pending one,
		// so a new image never queues behind records that cannot finish. When
		// no rate-limit slot is free it stays pending for the scheduled task.
		resolvePendingSynthIDRecord(ctx, s, rec)
	} else {
		_ = model.SetTaskDetectionStatus(rec.TaskID, model.TaskDetectionStatusChecked,
			"c2pa:"+rec.C2PASignal)
	}
}

// ProcessPendingSynthIDRecords drains deferred SynthID records up to batchSize.
// Each record's retained temp file is read, sent to the provenance API, then
// deleted. Respects per-minute and per-day rate limits; skips when rate-limited.
// channelIDs when non-empty restricts to those channels (targeted mode).
func ProcessPendingSynthIDRecords(ctx context.Context, batchSize int, channelIDs []int) (processed, skipped int) {
	s := operation_setting.GetImageDetectionSetting()
	records, err := model.GetPendingSynthIDRecords(batchSize, channelIDs)
	if err != nil {
		logger.LogWarn(ctx, common.LogText("image detection: pending query failed: %v", err))
		return 0, 0
	}

	for i, rec := range records {
		if ctx.Err() != nil {
			break
		}
		// Once the API has asked to slow down, the rest waits for a later run.
		if provenanceCoolingDown() {
			skipped += len(records) - i
			break
		}
		if resolvePendingSynthIDRecord(ctx, s, rec) {
			processed++
		} else {
			skipped++
		}
	}
	return processed, skipped
}

// resolvePendingSynthIDRecord runs L3 for one pending record: it reuses a
// cached result when the same image was already checked, otherwise takes a
// rate-limit slot, sends the retained image to the provenance API and deletes
// the temp file. It reports false when no slot was free or the API asked to
// slow down (the record stays pending), or the retained image was lost (the
// record is marked error).
func resolvePendingSynthIDRecord(ctx context.Context, s *operation_setting.ImageDetectionSetting, rec *model.ImageDetectionRecord) bool {
	// Try the cache first (a previous worker may have resolved the same image).
	if cached, ok := synthIDResultFromCache(ctx, rec.ContentHash); ok {
		_ = deleteRecordTempFile(rec)
		updates := cached.Columns()
		updates["synth_id_signal"] = cached.Signal
		updates["provenance_error"] = ""
		updates["provenance_error_detail"] = ""
		updates["temp_file_path"] = ""
		_ = model.UpdateImageDetectionRecord(rec.ID, updates)
		_ = model.SetTaskDetectionStatus(rec.TaskID, model.TaskDetectionStatusChecked,
			"synth_id:"+cached.Signal)
		return true
	}

	// Without a key the check cannot run. Keep the image so it can be checked
	// by hand on the official verify page, and do not spend a rate-limit slot.
	if provenanceAPIKey(s) == "" {
		_ = model.UpdateImageDetectionRecord(rec.ID, map[string]any{
			"synth_id_signal":  model.ImageDetectionSignalError,
			"provenance_error": model.ImageDetectionErrorNoAPIKey,
		})
		_ = model.SetTaskDetectionStatus(rec.TaskID, model.TaskDetectionStatusError,
			"synth_id:"+model.ImageDetectionSignalError)
		return true
	}

	// Nothing is sent while the wait the API asked for with its last 429
	// runs; the record stays pending, marked with why, for a later run.
	if provenanceCoolingDown() {
		_ = model.UpdateImageDetectionRecord(rec.ID, map[string]any{
			"provenance_error": model.ImageDetectionErrorRateLimited,
		})
		return false
	}

	ok, _ := acquireSynthIDSlot(ctx, s)
	if !ok {
		return false
	}

	imgBytes, readErr := os.ReadFile(rec.TempFilePath)
	if readErr != nil {
		// File gone — mark error and move on.
		_ = model.UpdateImageDetectionRecord(rec.ID, map[string]any{
			"synth_id_signal":  model.ImageDetectionSignalError,
			"provenance_error": model.ImageDetectionErrorImageLost,
			"temp_file_path":   "",
		})
		_ = model.SetTaskDetectionStatus(rec.TaskID, model.TaskDetectionStatusError, "temp_file_lost")
		return false
	}

	callStart := time.Now()
	outcome := callProvenanceAPI(ctx, imgBytes, s)
	if outcome.Error == model.ImageDetectionErrorRateLimited {
		provenanceCooldownUntil.Store(time.Now().Add(outcome.RetryAfter).UnixNano())
		_ = model.UpdateImageDetectionRecord(rec.ID, map[string]any{
			"provenance_error":        outcome.Error,
			"provenance_error_detail": outcome.Detail,
		})
		return false
	}
	if outcome.Cacheable {
		cacheSynthIDResult(ctx, rec.ContentHash, provenanceCacheEntry{Signal: outcome.Signal, ProvenanceAnswer: outcome.Answer})
	}
	updates := outcome.Answer.Columns()
	updates["synth_id_signal"] = outcome.Signal
	updates["provenance_error"] = outcome.Error
	updates["provenance_error_detail"] = outcome.Detail
	updates["duration_ms"] = rec.DurationMs + time.Since(callStart).Milliseconds()
	// A finished check no longer needs the image. One that could not finish
	// keeps it so it can be checked by hand on the official verify page.
	if outcome.Error == "" {
		_ = os.Remove(rec.TempFilePath)
		updates["temp_file_path"] = ""
	}
	_ = model.UpdateImageDetectionRecord(rec.ID, updates)
	status := model.TaskDetectionStatusChecked
	if outcome.Signal == model.ImageDetectionSignalError {
		status = model.TaskDetectionStatusError
	}
	_ = model.SetTaskDetectionStatus(rec.TaskID, status, "synth_id:"+outcome.Signal)
	return true
}

// --- L1: resolution -----------------------------------------------------------

func checkResolution(imgBytes []byte, requestSize string) (actual string, match bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(imgBytes))
	if err != nil {
		return "", false
	}
	actual = fmt.Sprintf("%dx%d", cfg.Width, cfg.Height)
	if strings.TrimSpace(requestSize) == "" || strings.ToLower(requestSize) == "auto" {
		return actual, false
	}
	parts := strings.SplitN(requestSize, "x", 2)
	if len(parts) != 2 {
		return actual, false
	}
	wantW, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	wantH, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return actual, false
	}
	return actual, cfg.Width == wantW && cfg.Height == wantH
}

// --- L2: C2PA via c2patool ---------------------------------------------------

// runC2PACheck writes imgBytes to a temp file, invokes c2patool, and parses
// the JSON report into one of the ImageDetectionSignal* constants plus the
// reason code that explains an "error" or a manifest that did not count.
// Returns ImageDetectionSignalError when the tool is missing, times out, or the
// temp file cannot be written, so a broken setup is not reported as "no signal".
func runC2PACheck(ctx context.Context, imgBytes []byte, toolPath string, timeout time.Duration) (signal, reason string) {
	toolPath = strings.TrimSpace(toolPath)
	if toolPath == "" {
		toolPath = "c2patool"
	}

	// c2patool requires a real file path with the right extension.
	tmpFile, err := os.CreateTemp("", "imgdet-*.jpg")
	if err != nil {
		return model.ImageDetectionSignalError, model.ImageDetectionErrorFailed
	}
	tmpPath := tmpFile.Name()
	defer os.Remove(tmpPath)
	if _, err = tmpFile.Write(imgBytes); err != nil {
		tmpFile.Close()
		return model.ImageDetectionSignalError, model.ImageDetectionErrorFailed
	}
	tmpFile.Close()

	tctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(tctx, toolPath, tmpPath) //nolint:gosec
	out, err := cmd.Output()
	if err != nil {
		// c2patool exits non-zero with "No claim found" when the image simply
		// carries no manifest; that is a real "no signal" result.
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && tctx.Err() == nil &&
			bytes.Contains(bytes.ToLower(exitErr.Stderr), []byte("no claim found")) {
			return model.ImageDetectionSignalNone, ""
		}
		switch {
		case errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist):
			return model.ImageDetectionSignalError, model.ImageDetectionErrorToolUnavailable
		case errors.Is(tctx.Err(), context.DeadlineExceeded):
			return model.ImageDetectionSignalError, model.ImageDetectionErrorTimeout
		default:
			return model.ImageDetectionSignalError, model.ImageDetectionErrorFailed
		}
	}
	return parseC2PAReport(out)
}

// parseC2PAReport reads the c2patool report (0.28 shape, abridged):
//
//	{
//	  "active_manifest": "urn:c2pa:...",
//	  "manifests": {
//	    "urn:c2pa:...": {
//	      "signature_info": {"issuer": "OpenAI OpCo, LLC", ...},
//	      "assertions": [{"label": "c2pa.actions.v2", "data": {"actions": [
//	        {"action": "c2pa.created", "digitalSourceType": ".../trainedAlgorithmicMedia"}]}}]
//	    }
//	  },
//	  "validation_state": "Trusted" | "Valid" | "Invalid"
//	}
//
// It applies the rule the Content Provenance API uses for its own C2PA result,
// because a detected manifest lets the pipeline skip that official check: the
// active manifest must be issued by OpenAI, record an AI generation action
// (trainedAlgorithmicMedia, including compositeWithTrainedAlgorithmicMedia
// for edits, or compositeSynthetic) and validate as trusted. "Valid" alone is
// not enough here, since any self-signed certificate naming OpenAI passes it.
// Otherwise the signal is "none" with the reason the manifest did not count.
func parseC2PAReport(out []byte) (signal, reason string) {
	var report struct {
		ActiveManifest  string `json:"active_manifest"`
		ValidationState string `json:"validation_state"`
		Manifests       map[string]struct {
			SignatureInfo struct {
				Issuer string `json:"issuer"`
			} `json:"signature_info"`
			Assertions []struct {
				Data struct {
					Actions []struct {
						DigitalSourceType string `json:"digitalSourceType"`
					} `json:"actions"`
				} `json:"data"`
			} `json:"assertions"`
		} `json:"manifests"`
	}
	if err := common.Unmarshal(out, &report); err != nil {
		return model.ImageDetectionSignalError, model.ImageDetectionErrorFailed
	}
	manifest, ok := report.Manifests[report.ActiveManifest]
	if !ok {
		return model.ImageDetectionSignalNone, ""
	}
	if strings.EqualFold(report.ValidationState, "invalid") {
		return model.ImageDetectionSignalNone, model.ImageDetectionReasonInvalidCredentials
	}
	if !strings.Contains(strings.ToLower(manifest.SignatureInfo.Issuer), "openai") {
		return model.ImageDetectionSignalNone, model.ImageDetectionReasonOtherIssuer
	}
	aiGenerated := false
	for _, a := range manifest.Assertions {
		for _, act := range a.Data.Actions {
			dst := strings.ToLower(act.DigitalSourceType)
			if strings.Contains(dst, "trainedalgorithmicmedia") || strings.Contains(dst, "compositesynthetic") {
				aiGenerated = true
			}
		}
	}
	if !aiGenerated {
		return model.ImageDetectionSignalNone, model.ImageDetectionReasonNoAIAction
	}
	if !strings.EqualFold(report.ValidationState, "trusted") {
		return model.ImageDetectionSignalNone, model.ImageDetectionReasonUntrustedCredentials
	}
	return model.ImageDetectionSignalDetected, ""
}

// --- L3: OpenAI Content Provenance API ----------------------------------------

// callProvenanceAPI calls POST /v1/content_provenance_checks with the image as
// the multipart "file" part. The key comes from the settings page, then the
// OPENAI_API_KEY_DETECTION and OPENAI_API_KEY environment variables; without
// one the call is skipped and "error" is returned. A server error or a network
// failure is retried up to provenanceMaxAttempts times with a growing wait.
func callProvenanceAPI(ctx context.Context, imgBytes []byte, s *operation_setting.ImageDetectionSetting) provenanceOutcome {
	key := provenanceAPIKey(s)
	if key == "" {
		return provenanceOutcome{Signal: model.ImageDetectionSignalError, Error: model.ImageDetectionErrorNoAPIKey}
	}

	// The API reads the file part's media type, and only PNG, JPEG and WebP
	// images are supported; anything else would be rejected with a 400, so it
	// is not worth spending a strictly rate-limited call on.
	mediaType := http.DetectContentType(imgBytes)
	ext, supported := provenanceImageExtensions[mediaType]
	if !supported {
		logger.LogWarn(ctx, common.LogText("image detection: SynthID check skipped, unsupported image type %s", mediaType))
		return provenanceOutcome{Signal: model.ImageDetectionSignalError,
			Error: model.ImageDetectionErrorUnsupportedImage, Detail: mediaType}
	}
	// The API refuses files over 50 MiB, so they are not worth a call either.
	if len(imgBytes) > provenanceMaxImageBytes {
		return provenanceOutcome{Signal: model.ImageDetectionSignalError, Error: model.ImageDetectionErrorTooLarge,
			Detail: fmt.Sprintf("%.1f MiB", float64(len(imgBytes))/(1<<20))}
	}

	// Build multipart/form-data body.
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	partHeader := make(textproto.MIMEHeader)
	partHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name="file"; filename="image%s"`, ext))
	partHeader.Set("Content-Type", mediaType)
	fw, err := mw.CreatePart(partHeader)
	if err == nil {
		_, err = fw.Write(imgBytes)
	}
	if err == nil {
		err = mw.Close()
	}
	if err != nil {
		return provenanceOutcome{Signal: model.ImageDetectionSignalError,
			Error: model.ImageDetectionErrorFailed, Detail: truncateDetectionDetail(err.Error())}
	}

	var outcome provenanceOutcome
	for attempt := 1; attempt <= provenanceMaxAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return outcome
			case <-time.After(time.Duration(attempt-1) * provenanceRetryBackoff):
			}
		}
		var retry bool
		outcome, retry = postProvenanceCheck(ctx, buf.Bytes(), mw.FormDataContentType(), key)
		if !retry {
			break
		}
	}
	return outcome
}

// postProvenanceCheck sends one provenance check and classifies the answer by
// the documented statuses. retry reports a failure worth another attempt: a
// 5xx status, or a network error other than the request timing out.
func postProvenanceCheck(ctx context.Context, body []byte, contentType, key string) (outcome provenanceOutcome, retry bool) {
	reqCtx, cancel := context.WithTimeout(ctx, synthIDRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, provenanceAPIURL, bytes.NewReader(body))
	if err != nil {
		return provenanceOutcome{Signal: model.ImageDetectionSignalError,
			Error: model.ImageDetectionErrorFailed, Detail: truncateDetectionDetail(err.Error())}, false
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Authorization", "Bearer "+key)

	client := GetSSRFProtectedHTTPClient()
	resp, err := client.Do(req)
	if err != nil {
		logger.LogWarn(ctx, common.LogText("image detection: SynthID request failed: %v", err))
		if errors.Is(reqCtx.Err(), context.DeadlineExceeded) {
			return provenanceOutcome{Signal: model.ImageDetectionSignalError,
				Error: model.ImageDetectionErrorTimeout, Detail: truncateDetectionDetail(err.Error())}, false
		}
		return provenanceOutcome{Signal: model.ImageDetectionSignalError,
			Error: model.ImageDetectionErrorRequestFailed, Detail: truncateDetectionDetail(err.Error())}, ctx.Err() == nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		logger.LogWarn(ctx, common.LogText("image detection: SynthID API returned HTTP %d: %s",
			resp.StatusCode, common.LocalLogPreview(string(snippet))))
		// Show the API's own error message rather than the raw body.
		message := strings.TrimSpace(string(snippet))
		var apiErr struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if common.Unmarshal(snippet, &apiErr) == nil && apiErr.Error.Message != "" {
			message = apiErr.Error.Message
		}
		outcome = provenanceOutcome{Signal: model.ImageDetectionSignalError, Error: model.ImageDetectionErrorHTTP,
			Detail: truncateDetectionDetail(fmt.Sprintf("HTTP %d: %s", resp.StatusCode, message))}
		switch {
		case resp.StatusCode == http.StatusBadRequest:
			outcome.Error = model.ImageDetectionErrorRejected
		case resp.StatusCode == http.StatusUnauthorized:
			outcome.Error = model.ImageDetectionErrorInvalidAPIKey
		case resp.StatusCode == http.StatusNotFound:
			outcome.Error = model.ImageDetectionErrorNoAccess
		case resp.StatusCode == http.StatusRequestEntityTooLarge:
			outcome.Error = model.ImageDetectionErrorTooLarge
		case resp.StatusCode == http.StatusTooManyRequests:
			// A rate limit says nothing about the image, so the check stays
			// pending. Retry-After is in seconds; without one, wait a minute.
			outcome.Signal = model.ImageDetectionSignalPending
			outcome.Error = model.ImageDetectionErrorRateLimited
			outcome.RetryAfter = time.Minute
			if seconds, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && seconds > 0 {
				outcome.RetryAfter = min(time.Duration(seconds)*time.Second, 24*time.Hour)
			}
		case resp.StatusCode >= http.StatusInternalServerError:
			outcome.Error = model.ImageDetectionErrorServerError
			return outcome, true
		}
		return outcome, false
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return provenanceOutcome{Signal: model.ImageDetectionSignalError,
			Error: model.ImageDetectionErrorRequestFailed, Detail: truncateDetectionDetail(err.Error())}, ctx.Err() == nil
	}
	outcome = provenanceOutcome{Cacheable: true}
	outcome.Signal, outcome.Answer = parseProvenanceResponse(respBody)
	if outcome.Signal == model.ImageDetectionSignalInconclusive {
		outcome.Error = model.ImageDetectionErrorInconclusive
		outcome.Detail = truncateDetectionDetail(string(respBody))
	}
	return outcome, false
}

// provenanceAPIKey returns the key for the Content Provenance API: the one
// saved in the settings page first, then the environment variables. Any
// OpenAI platform key with access works, whichever key generated the image,
// because the check reads the signals embedded in the image itself.
func provenanceAPIKey(s *operation_setting.ImageDetectionSetting) string {
	if key := strings.TrimSpace(s.ProvenanceAPIKey); key != "" {
		return key
	}
	if key := strings.TrimSpace(os.Getenv("OPENAI_API_KEY_DETECTION")); key != "" {
		return key
	}
	return strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
}

// truncateDetectionDetail bounds a failure detail to the stored column size
// without splitting a UTF-8 character.
func truncateDetectionDetail(detail string) string {
	if cut := truncateUTF8(detail, 500); cut != detail {
		return cut + "…"
	}
	return detail
}

// truncateUTF8 cuts s to at most maxBytes without splitting a UTF-8
// character, so a value always fits its varchar column on MySQL and
// PostgreSQL, which reject over-long strings.
func truncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// parseProvenanceResponse reads the Content Provenance answer:
//
//	{
//	  "object": "provenance_check",
//	  "created_at": 1779225920,
//	  "results": [
//	    {"type": "c2pa", "outcome": "detected", "validation_state": "trusted", "issuer": "OpenAI OpCo, LLC"},
//	    {"type": "synthid", "outcome": "not_detected"}
//	  ]
//	}
//
// The API already folds the issuer, the AI generation action and the
// validation state into the C2PA outcome, and reads SynthID on its own, so
// "detected" on either result → ImageDetectionSignalDetected; otherwise any
// "not_detected" → ImageDetectionSignalNotDetected; no usable result →
// ImageDetectionSignalInconclusive. The per-signal answer is returned too,
// cut to its column sizes.
func parseProvenanceResponse(body []byte) (string, model.ProvenanceAnswer) {
	var resp struct {
		Results []struct {
			Type            string `json:"type"`
			Outcome         string `json:"outcome"`
			ValidationState string `json:"validation_state"`
			Issuer          string `json:"issuer"`
			Model           string `json:"model"`
		} `json:"results"`
	}
	var answer model.ProvenanceAnswer
	if err := common.Unmarshal(body, &resp); err != nil {
		return model.ImageDetectionSignalInconclusive, answer
	}
	signal := model.ImageDetectionSignalInconclusive
	for _, r := range resp.Results {
		outcome := strings.ToLower(r.Outcome)
		switch strings.ToLower(r.Type) {
		case "c2pa":
			answer.C2PA = truncateUTF8(outcome, 32)
			answer.C2PAState = truncateUTF8(strings.ToLower(r.ValidationState), 32)
			answer.Issuer = truncateUTF8(r.Issuer, 128)
		case "synthid":
			answer.SynthID = truncateUTF8(outcome, 32)
		}
		if answer.Model == "" {
			answer.Model = truncateUTF8(r.Model, 64)
		}
		switch {
		case outcome == "detected":
			signal = model.ImageDetectionSignalDetected
		case outcome == "not_detected" && signal != model.ImageDetectionSignalDetected:
			signal = model.ImageDetectionSignalNotDetected
		}
	}
	return signal, answer
}

// provenanceOutcome is the result of one Content Provenance API call. Error
// and Detail explain an "error" signal; only answered calls are cacheable.
type provenanceOutcome struct {
	Signal    string
	Cacheable bool
	Error     string
	Detail    string
	// Answer is the per-signal result when the API answered.
	Answer model.ProvenanceAnswer
	// RetryAfter is how long the API asked to wait with a 429.
	RetryAfter time.Duration
}

// provenanceRetryBackoff spaces the retries of a call that hit a server error
// or a network failure: the n-th retry waits n times this long.
var provenanceRetryBackoff = 2 * time.Second

// provenanceCooldownUntil holds, in Unix nanoseconds, the end of the wait the
// API asked for with its last 429. No call is sent before then.
var provenanceCooldownUntil atomic.Int64

// provenanceCoolingDown reports whether the wait from the API's last 429 is
// still running.
func provenanceCoolingDown() bool {
	return time.Now().UnixNano() < provenanceCooldownUntil.Load()
}

// Images kept for manual verification are bounded, so a deployment that runs
// detection without a key cannot fill the disk.
const (
	retainedImageMaxCount = 200
	retainedImageMaxAge   = 72 * time.Hour
)

// pruneRetainedDetectionImages deletes images kept for manual verification
// beyond the newest maxCount or older than maxAge.
func pruneRetainedDetectionImages(maxCount int, maxAge time.Duration) {
	records, err := model.GetExpiredRetainedImages(time.Now().Add(-maxAge).Unix(), maxCount, 50)
	if err != nil {
		logger.LogWarn(context.Background(), common.LogText("image detection: prune retained images failed: %v", err))
		return
	}
	for _, rec := range records {
		_ = deleteRecordTempFile(rec)
		_ = model.UpdateImageDetectionRecord(rec.ID, map[string]any{"temp_file_path": ""})
	}
}

// --- Rate limiting ------------------------------------------------------------

// synthIDMemoryLimiter enforces the SynthID call caps when Redis is not
// configured, the same fallback the request rate limiter uses. It holds two
// fixed keys and is initialized without idle eviction, because evicting the
// day key after a quiet period would reset the daily cap. Its day window is a
// rolling 24 hours rather than the calendar day used by the Redis path.
var synthIDMemoryLimiter common.InMemoryRateLimiter

func acquireSynthIDSlot(ctx context.Context, s *operation_setting.ImageDetectionSetting) (bool, time.Duration) {
	if common.RDB == nil {
		synthIDMemoryLimiter.Init(0)
		minute := synthIDMemoryLimiter.Reserve("minute", s.SynthIDRatePerMinute, 60)
		if minute == nil {
			return false, 60 * time.Second
		}
		day := synthIDMemoryLimiter.Reserve("day", s.SynthIDRatePerDay, 24*60*60)
		if day == nil {
			minute.Complete(false)
			return false, 24 * time.Hour
		}
		minute.Complete(true)
		day.Complete(true)
		return true, 0
	}
	now := time.Now()
	minKey := synthIDRLMinutePrefix + strconv.FormatInt(now.Unix()/60, 10)
	dayKey := synthIDRLDayPrefix + now.Format("2006-01-02")

	minCount, err := common.RDB.Incr(ctx, minKey).Result()
	if err != nil {
		return false, 5 * time.Second
	}
	if minCount == 1 {
		_ = common.RDB.Expire(ctx, minKey, 70*time.Second)
	}
	if int(minCount) > s.SynthIDRatePerMinute {
		ttl, _ := common.RDB.TTL(ctx, minKey).Result()
		if ttl <= 0 {
			ttl = 60 * time.Second
		}
		_ = common.RDB.Decr(ctx, minKey)
		return false, ttl
	}

	dayCount, err := common.RDB.Incr(ctx, dayKey).Result()
	if err != nil {
		_ = common.RDB.Decr(ctx, minKey)
		return false, 5 * time.Second
	}
	if dayCount == 1 {
		_ = common.RDB.Expire(ctx, dayKey, 25*time.Hour)
	}
	if int(dayCount) > s.SynthIDRatePerDay {
		_ = common.RDB.Decr(ctx, dayKey)
		_ = common.RDB.Decr(ctx, minKey)
		return false, 24 * time.Hour
	}
	return true, 0
}

// --- Content-hash cache -------------------------------------------------------

func imageContentHash(b []byte) string {
	h := sha256.Sum256(b)
	return fmt.Sprintf("%x", h)
}

// provenanceCacheEntry is a Content Provenance answer cached for 24 hours,
// keyed by the image's content hash.
type provenanceCacheEntry struct {
	Signal string `json:"signal"`
	model.ProvenanceAnswer
}

func synthIDResultFromCache(ctx context.Context, hash string) (provenanceCacheEntry, bool) {
	if common.RDB == nil || hash == "" {
		return provenanceCacheEntry{}, false
	}
	val, err := common.RDB.Get(ctx, synthIDResultCachePrefix+hash).Result()
	if err != nil || val == "" {
		return provenanceCacheEntry{}, false
	}
	var entry provenanceCacheEntry
	if common.UnmarshalJsonStr(val, &entry) != nil {
		// Entries cached before the per-signal answer was kept hold the signal alone.
		entry = provenanceCacheEntry{Signal: val}
	}
	return entry, entry.Signal != ""
}

func cacheSynthIDResult(ctx context.Context, hash string, entry provenanceCacheEntry) {
	if common.RDB == nil || hash == "" || entry.Signal == "" {
		return
	}
	value, err := common.Marshal(entry)
	if err != nil {
		return
	}
	_ = common.RDB.Set(ctx, synthIDResultCachePrefix+hash, string(value), 24*time.Hour)
}

// --- Temp-file helpers --------------------------------------------------------

func retainImageToTempFile(imgBytes []byte) (string, error) {
	f, err := os.CreateTemp("", "imgdet-retain-*.jpg")
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err = f.Write(imgBytes); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func deleteRecordTempFile(rec *model.ImageDetectionRecord) error {
	if rec.TempFilePath == "" {
		return nil
	}
	return os.Remove(rec.TempFilePath)
}
