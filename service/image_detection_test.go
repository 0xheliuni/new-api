package service

import (
	"context"
	"fmt"
	"io"
	"maps"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/setting/system_setting"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// fakeC2PAToolEnv turns the test binary into a stand-in c2patool that prints
// the report held in the variable, so the C2PA layer can be exercised without
// the real tool installed.
const fakeC2PAToolEnv = "IMGDET_FAKE_C2PATOOL"

func init() {
	report := os.Getenv(fakeC2PAToolEnv)
	if report == "" {
		return
	}
	fmt.Print(report)
	os.Exit(0)
}

const (
	openAIIssuer = "OpenAI OpCo, LLC"
	aiGenerated  = "http://cv.iptc.org/newscodes/digitalsourcetype/trainedAlgorithmicMedia"
)

// c2paReport builds a report shaped like c2patool 0.28 output for a gpt-image
// result: the active manifest's signer, its creation action and the overall
// validation state.
func c2paReport(issuer, digitalSourceType, validationState string) string {
	return fmt.Sprintf(`{"active_manifest":"urn:c2pa:m","manifests":{"urn:c2pa:m":{`+
		`"claim_generator_info":[{"name":"OpenAI Media Service API"}],`+
		`"signature_info":{"alg":"Es256","issuer":%q,"common_name":"OpenAI Media Service"},`+
		`"assertions":[{"label":"c2pa.actions.v2","data":{"actions":[`+
		`{"action":"c2pa.created","digitalSourceType":%q},{"action":"c2pa.converted"}]}}]}},`+
		`"validation_state":%q}`, issuer, digitalSourceType, validationState)
}

// setupImageDetectionTest runs the SynthID layer on its own in-memory database,
// without Redis and without an OpenAI key, so a provenance call resolves to
// "error" deterministically and never reaches the network. L1/L2 are disabled
// to keep the cases on L3.
func setupImageDetectionTest(t *testing.T) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&model.ImageDetectionRecord{}))

	prevDB := model.DB
	model.DB = db
	prevRDB, prevRedisEnabled := common.RDB, common.RedisEnabled
	common.RDB, common.RedisEnabled = nil, false
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_API_KEY_DETECTION", "")

	setting := operation_setting.GetImageDetectionSetting()
	prevSetting := *setting
	setting.CheckResolution = false
	setting.CheckC2PA = false
	setting.CheckSynthID = true
	// Retries run without waiting, and no earlier 429 holds calls back.
	prevBackoff := provenanceRetryBackoff
	provenanceRetryBackoff = 0
	provenanceCooldownUntil.Store(0)

	t.Cleanup(func() {
		var records []*model.ImageDetectionRecord
		db.Find(&records)
		for _, rec := range records {
			if rec.TempFilePath != "" {
				_ = os.Remove(rec.TempFilePath)
			}
		}
		*setting = prevSetting
		provenanceRetryBackoff = prevBackoff
		provenanceCooldownUntil.Store(0)
		common.RDB, common.RedisEnabled = prevRDB, prevRedisEnabled
		model.DB = prevDB
		_ = sqlDB.Close()
	})
}

func insertPendingSynthIDRecord(t *testing.T, requestID string) *model.ImageDetectionRecord {
	t.Helper()
	imgPath := filepath.Join(t.TempDir(), requestID+".jpg")
	require.NoError(t, os.WriteFile(imgPath, []byte(requestID), 0o600))
	rec := &model.ImageDetectionRecord{
		RequestID:     requestID,
		ContentHash:   "hash-" + requestID,
		C2PASignal:    model.ImageDetectionSignalNone,
		SynthIDSignal: model.ImageDetectionSignalPending,
		TempFilePath:  imgPath,
	}
	require.NoError(t, model.InsertImageDetectionRecord(rec))
	return rec
}

func TestProcessPendingSynthIDRecordsResolvesWithoutRedis(t *testing.T) {
	setupImageDetectionTest(t)
	useProvenanceStub(t)
	rec := insertPendingSynthIDRecord(t, "req-pending")
	require.NoError(t, os.WriteFile(rec.TempFilePath, []byte(pngImage), 0o600))

	processed, skipped := ProcessPendingSynthIDRecords(context.Background(), 10, nil)

	assert.Equal(t, 1, processed)
	assert.Equal(t, 0, skipped)
	var got model.ImageDetectionRecord
	require.NoError(t, model.DB.First(&got, rec.ID).Error)
	assert.Equal(t, model.ImageDetectionSignalDetected, got.SynthIDSignal)
	// A finished check no longer needs the retained image.
	assert.Empty(t, got.TempFilePath)
	assert.NoFileExists(t, rec.TempFilePath)
}

func TestRunImageDetectionPipelineResolvesItsOwnRecordBehindOlderPending(t *testing.T) {
	setupImageDetectionTest(t)
	insertPendingSynthIDRecord(t, "req-older")

	RunImageDetectionPipeline(ImageDetectionJob{
		RequestID:  "req-new",
		ImageBytes: []byte("new image"),
		Trigger:    ImageDetectionTriggerGlobal,
	})

	var got model.ImageDetectionRecord
	require.NoError(t, model.DB.Where("request_id = ?", "req-new").First(&got).Error)
	assert.Equal(t, model.ImageDetectionSignalError, got.SynthIDSignal)
	assert.Equal(t, model.ImageDetectionErrorNoAPIKey, got.ProvenanceError)
}

func TestRunImageDetectionPipelineForceSynthIDAfterC2PADetected(t *testing.T) {
	tests := []struct {
		name  string
		force bool
		want  string
	}{
		{name: "a C2PA signal skips SynthID by default", force: false, want: model.ImageDetectionSignalSkipped},
		// No API key is configured, so a SynthID call that actually runs ends in error.
		{name: "forced SynthID still runs after a C2PA signal", force: true, want: model.ImageDetectionSignalError},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupImageDetectionTest(t)
			t.Setenv(fakeC2PAToolEnv, c2paReport(openAIIssuer, aiGenerated, "Trusted"))
			setting := operation_setting.GetImageDetectionSetting()
			setting.CheckC2PA = true
			setting.C2PAToolPath = os.Args[0]
			setting.C2PATimeoutSeconds = 30
			setting.SynthIDForce = tc.force

			RunImageDetectionPipeline(ImageDetectionJob{
				RequestID:  "req-forced",
				ImageBytes: []byte("image"),
				Trigger:    ImageDetectionTriggerGlobal,
			})

			var got model.ImageDetectionRecord
			require.NoError(t, model.DB.Where("request_id = ?", "req-forced").First(&got).Error)
			assert.Equal(t, model.ImageDetectionSignalDetected, got.C2PASignal)
			assert.Equal(t, tc.want, got.SynthIDSignal)
		})
	}
}

func TestDetectionStatusesLetFinishedSynthIDDecideTheVerdict(t *testing.T) {
	setupImageDetectionTest(t)
	cases := []struct {
		requestID string
		synthID   string
		verdict   string
		status    string
	}{
		{"req-c2pa-only", model.ImageDetectionSignalSkipped, model.ImageDetectionVerdictPass, model.TaskDetectionStatusChecked},
		{"req-synthid-missing", model.ImageDetectionSignalNotDetected, model.ImageDetectionVerdictFail, model.TaskDetectionStatusChecked},
		{"req-synthid-pending", model.ImageDetectionSignalPending, "", model.TaskDetectionStatusChecking},
	}
	requestIDs := make([]string, 0, len(cases))
	for _, tc := range cases {
		require.NoError(t, model.InsertImageDetectionRecord(&model.ImageDetectionRecord{
			RequestID:     tc.requestID,
			C2PASignal:    model.ImageDetectionSignalDetected,
			SynthIDSignal: tc.synthID,
		}))
		requestIDs = append(requestIDs, tc.requestID)
	}

	results, err := model.GetDetectionStatusesByRequestIDs(requestIDs)

	require.NoError(t, err)
	for _, tc := range cases {
		assert.Equal(t, tc.verdict, results[tc.requestID].Verdict, tc.requestID)
		assert.Equal(t, tc.status, results[tc.requestID].Status, tc.requestID)
	}
}

func TestDetectionStatusesReportTheDetectionDuration(t *testing.T) {
	setupImageDetectionTest(t)
	require.NoError(t, model.InsertImageDetectionRecord(&model.ImageDetectionRecord{
		RequestID:     "req-duration",
		C2PASignal:    model.ImageDetectionSignalDetected,
		SynthIDSignal: model.ImageDetectionSignalNotDetected,
		DurationMs:    3210,
	}))

	results, err := model.GetDetectionStatusesByRequestIDs([]string{"req-duration"})

	require.NoError(t, err)
	assert.Equal(t, int64(3210), results["req-duration"].DurationMs)
}

func TestRunImageDetectionPipelineRecordsDetectionDuration(t *testing.T) {
	setupImageDetectionTest(t)
	t.Setenv(fakeC2PAToolEnv, "1")
	setting := operation_setting.GetImageDetectionSetting()
	setting.CheckC2PA = true
	setting.C2PAToolPath = os.Args[0]
	setting.C2PATimeoutSeconds = 30
	setting.CheckSynthID = false

	RunImageDetectionPipeline(ImageDetectionJob{
		RequestID:  "req-timed",
		ImageBytes: []byte("image"),
		Trigger:    ImageDetectionTriggerGlobal,
	})

	var got model.ImageDetectionRecord
	require.NoError(t, model.DB.Where("request_id = ?", "req-timed").First(&got).Error)
	// The stand-in c2patool runs as a child process, so the check takes time.
	assert.Positive(t, got.DurationMs)
}

func TestProcessPendingSynthIDRecordsAddsToEarlierDetectionDuration(t *testing.T) {
	setupImageDetectionTest(t)
	useProvenanceStub(t)
	rec := insertPendingSynthIDRecord(t, "req-deferred")
	require.NoError(t, os.WriteFile(rec.TempFilePath, []byte(pngImage), 0o600))
	require.NoError(t, model.UpdateImageDetectionRecord(rec.ID, map[string]any{"duration_ms": 1000}))

	ProcessPendingSynthIDRecords(context.Background(), 10, nil)

	var got model.ImageDetectionRecord
	require.NoError(t, model.DB.First(&got, rec.ID).Error)
	assert.Equal(t, model.ImageDetectionSignalDetected, got.SynthIDSignal)
	assert.GreaterOrEqual(t, got.DurationMs, int64(1000))
}

// useLogTable points the log database at the test database and creates the
// logs table, so the usage-log queries can be exercised end to end.
func useLogTable(t *testing.T) {
	t.Helper()
	require.NoError(t, model.DB.AutoMigrate(&model.Log{}))
	prevLogDB := model.LOG_DB
	model.LOG_DB = model.DB
	t.Cleanup(func() { model.LOG_DB = prevLogDB })
}

func TestGetAllLogsAttachesImageDetectionSteps(t *testing.T) {
	setupImageDetectionTest(t)
	useLogTable(t)
	require.NoError(t, model.DB.Create(&model.Log{
		Type:      model.LogTypeConsume,
		RequestId: "req-log",
		ModelName: "gpt-image-1",
		CreatedAt: 1793188800,
	}).Error)
	require.NoError(t, model.InsertImageDetectionRecord(&model.ImageDetectionRecord{
		RequestID:     "req-log",
		C2PASignal:    model.ImageDetectionSignalDetected,
		SynthIDSignal: model.ImageDetectionSignalNotDetected,
		DurationMs:    3210,
	}))

	logs, _, err := model.GetAllLogs(model.LogTypeConsume, 0, 0, "", "", "", 0, 10, 0, "", "", "")

	require.NoError(t, err)
	require.Len(t, logs, 1)
	var other struct {
		DetectionVerdict string `json:"detection_verdict"`
		DetectionSteps   struct {
			Metadata   string `json:"metadata"`
			Content    string `json:"content"`
			DurationMs int64  `json:"duration_ms"`
		} `json:"detection_steps"`
	}
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	assert.Equal(t, model.ImageDetectionVerdictFail, other.DetectionVerdict)
	assert.Equal(t, model.ImageDetectionStepPass, other.DetectionSteps.Metadata)
	assert.Equal(t, model.ImageDetectionStepFail, other.DetectionSteps.Content)
	assert.Equal(t, int64(3210), other.DetectionSteps.DurationMs)
}

func TestGetAllLogsAttachesImageDetectionFailureReasons(t *testing.T) {
	setupImageDetectionTest(t)
	useLogTable(t)
	require.NoError(t, model.DB.Create(&model.Log{
		Type:      model.LogTypeConsume,
		RequestId: "req-failed",
		ModelName: "gpt-image-1",
		CreatedAt: 1793188800,
	}).Error)
	rec := insertRetainedImageRecord(t, "req-failed", 1793188800)
	require.NoError(t, model.UpdateImageDetectionRecord(rec.ID, map[string]any{
		"c2_pa_signal":            model.ImageDetectionSignalError,
		"c2pa_error":              model.ImageDetectionErrorToolUnavailable,
		"provenance_error_detail": "detail text",
	}))

	logs, _, err := model.GetAllLogs(model.LogTypeConsume, 0, 0, "", "", "", 0, 10, 0, "", "", "")

	require.NoError(t, err)
	require.Len(t, logs, 1)
	var other struct {
		DetectionSteps struct {
			MetadataError      string `json:"metadata_error"`
			ContentError       string `json:"content_error"`
			ContentErrorDetail string `json:"content_error_detail"`
			ImageAvailable     bool   `json:"image_available"`
		} `json:"detection_steps"`
	}
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	assert.Equal(t, model.ImageDetectionErrorToolUnavailable, other.DetectionSteps.MetadataError)
	assert.Equal(t, model.ImageDetectionErrorNoAPIKey, other.DetectionSteps.ContentError)
	assert.Equal(t, "detail text", other.DetectionSteps.ContentErrorDetail)
	assert.True(t, other.DetectionSteps.ImageAvailable)
	assert.NotContains(t, logs[0].Other, rec.TempFilePath)
}

// provenanceStub stands in for the Content Provenance endpoint: it records the
// request and answers with the documented response, or with the configured
// status, body, headers or transport error. With failures set, only the first
// calls get the configured answer and later calls get the documented one.
type provenanceStub struct {
	calls         int
	authorization string
	partType      string
	fileName      string
	status        int
	body          string
	header        http.Header
	err           error
	failures      int
}

// documentedAnswer is the example response in the Content Provenance docs.
const documentedAnswer = `{"object":"provenance_check","created_at":1779225920,"results":[` +
	`{"type":"c2pa","outcome":"detected","validation_state":"trusted","issuer":"OpenAI OpCo, LLC"},` +
	`{"type":"synthid","outcome":"not_detected"}]}`

func (s *provenanceStub) RoundTrip(req *http.Request) (*http.Response, error) {
	s.calls++
	s.authorization = req.Header.Get("Authorization")
	configured := s.failures == 0 || s.calls <= s.failures
	if configured && s.err != nil {
		return nil, s.err
	}
	reader, err := req.MultipartReader()
	if err != nil {
		return nil, err
	}
	part, err := reader.NextPart()
	if err != nil {
		return nil, err
	}
	s.partType = part.Header.Get("Content-Type")
	s.fileName = part.FileName()
	status, body := http.StatusOK, documentedAnswer
	header := http.Header{"Content-Type": []string{"application/json"}}
	if configured && s.status != 0 {
		status, body = s.status, s.body
		maps.Copy(header, s.header)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    req,
	}, nil
}

// pngImage starts with the PNG signature, which is all the media type sniffing
// and the stubbed endpoint need.
const pngImage = "\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"

func useProvenanceStub(t *testing.T) *provenanceStub {
	t.Helper()
	fetchSetting := system_setting.GetFetchSetting()
	prevFetchSetting := *fetchSetting
	prevHTTPClient := httpClient
	t.Cleanup(func() {
		*fetchSetting = prevFetchSetting
		httpClient = prevHTTPClient
	})
	fetchSetting.EnableSSRFProtection = false
	stub := &provenanceStub{}
	httpClient = &http.Client{Transport: stub}
	t.Setenv("OPENAI_API_KEY_DETECTION", "test-key")
	return stub
}

func TestCallProvenanceAPISendsTheImageMediaType(t *testing.T) {
	tests := []struct {
		name     string
		image    string
		partType string
		fileName string
	}{
		{name: "png", image: pngImage, partType: "image/png", fileName: "image.png"},
		{name: "jpeg", image: "\xff\xd8\xff\xe0\x00\x10JFIF\x00", partType: "image/jpeg", fileName: "image.jpg"},
		{name: "webp", image: "RIFF\x24\x00\x00\x00WEBPVP8 ", partType: "image/webp", fileName: "image.webp"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupImageDetectionTest(t)
			stub := useProvenanceStub(t)

			outcome := callProvenanceAPI(context.Background(), []byte(tc.image), operation_setting.GetImageDetectionSetting())

			require.Equal(t, 1, stub.calls)
			assert.Equal(t, tc.partType, stub.partType)
			assert.Equal(t, tc.fileName, stub.fileName)
			assert.Equal(t, model.ImageDetectionSignalDetected, outcome.Signal)
			assert.Empty(t, outcome.Error)
		})
	}
}

func TestCallProvenanceAPIPrefersTheKeySavedInSettings(t *testing.T) {
	setupImageDetectionTest(t)
	stub := useProvenanceStub(t)
	operation_setting.GetImageDetectionSetting().ProvenanceAPIKey = "settings-key"

	callProvenanceAPI(context.Background(), []byte(pngImage), operation_setting.GetImageDetectionSetting())

	assert.Equal(t, "Bearer settings-key", stub.authorization)
}

func TestCallProvenanceAPIReportsWhyTheCheckFailed(t *testing.T) {
	tests := []struct {
		name       string
		image      string
		noKey      bool
		stub       provenanceStub
		wantCalls  int
		wantError  string
		wantDetail string
	}{
		{name: "no key configured", image: pngImage, noKey: true, wantError: model.ImageDetectionErrorNoAPIKey},
		{name: "unsupported image", image: "GIF89a\x01\x00\x01\x00", wantError: model.ImageDetectionErrorUnsupportedImage, wantDetail: "image/gif"},
		{
			name:       "over the 50 MiB upload limit",
			image:      pngImage + strings.Repeat("\x00", 50<<20),
			wantError:  model.ImageDetectionErrorTooLarge,
			wantDetail: "MiB",
		},
		{
			name:       "key rejected",
			image:      pngImage,
			stub:       provenanceStub{status: http.StatusUnauthorized, body: `{"error":{"message":"Incorrect API key provided.","code":"invalid_api_key"}}`},
			wantCalls:  1,
			wantError:  model.ImageDetectionErrorInvalidAPIKey,
			wantDetail: "HTTP 401: Incorrect API key provided.",
		},
		{
			name:       "organization without access",
			image:      pngImage,
			stub:       provenanceStub{status: http.StatusNotFound, body: `{"error":{"message":"Not found.","code":"not_found"}}`},
			wantCalls:  1,
			wantError:  model.ImageDetectionErrorNoAccess,
			wantDetail: "HTTP 404: Not found.",
		},
		{
			name:       "image rejected",
			image:      pngImage,
			stub:       provenanceStub{status: http.StatusBadRequest, body: `{"error":{"message":"Unsupported image content.","code":"invalid_file"}}`},
			wantCalls:  1,
			wantError:  model.ImageDetectionErrorRejected,
			wantDetail: "HTTP 400: Unsupported image content.",
		},
		{
			name:       "server keeps failing",
			image:      pngImage,
			stub:       provenanceStub{status: http.StatusServiceUnavailable, body: "upstream unavailable"},
			wantCalls:  3,
			wantError:  model.ImageDetectionErrorServerError,
			wantDetail: "HTTP 503: upstream unavailable",
		},
		{
			name:       "API unreachable",
			image:      pngImage,
			stub:       provenanceStub{err: fmt.Errorf("dial tcp: connection refused")},
			wantCalls:  3,
			wantError:  model.ImageDetectionErrorRequestFailed,
			wantDetail: "connection refused",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setupImageDetectionTest(t)
			stub := useProvenanceStub(t)
			stub.status, stub.body, stub.err = tc.stub.status, tc.stub.body, tc.stub.err
			if tc.noKey {
				t.Setenv("OPENAI_API_KEY_DETECTION", "")
			}

			outcome := callProvenanceAPI(context.Background(), []byte(tc.image), operation_setting.GetImageDetectionSetting())

			assert.Equal(t, tc.wantCalls, stub.calls)
			assert.Equal(t, model.ImageDetectionSignalError, outcome.Signal)
			assert.Equal(t, tc.wantError, outcome.Error)
			assert.Contains(t, outcome.Detail, tc.wantDetail)
		})
	}
}

// Server errors are transient, so the call is retried before it gives up.
func TestCallProvenanceAPIRetriesAfterAServerError(t *testing.T) {
	setupImageDetectionTest(t)
	stub := useProvenanceStub(t)
	stub.status, stub.body, stub.failures = http.StatusServiceUnavailable, "busy", 1

	outcome := callProvenanceAPI(context.Background(), []byte(pngImage), operation_setting.GetImageDetectionSetting())

	assert.Equal(t, 2, stub.calls)
	assert.Equal(t, model.ImageDetectionSignalDetected, outcome.Signal)
	assert.Empty(t, outcome.Error)
}

// The API reads C2PA and SynthID independently; both answers are kept and
// shown, because re-encoding strips C2PA while SynthID survives.
func TestProcessPendingSynthIDRecordsKeepsEachProvenanceSignal(t *testing.T) {
	setupImageDetectionTest(t)
	useLogTable(t)
	stub := useProvenanceStub(t)
	stub.status = http.StatusOK
	stub.body = `{"object":"provenance_check","created_at":1779225920,"results":[` +
		`{"type":"c2pa","outcome":"detected","validation_state":"trusted","issuer":"OpenAI OpCo, LLC"},` +
		`{"type":"synthid","outcome":"detected","model":"gpt-image-2"}]}`
	require.NoError(t, model.DB.Create(&model.Log{
		Type:      model.LogTypeConsume,
		RequestId: "req-signals",
		ModelName: "gpt-image-2",
		CreatedAt: 1793188800,
	}).Error)
	rec := insertPendingSynthIDRecord(t, "req-signals")
	require.NoError(t, os.WriteFile(rec.TempFilePath, []byte(pngImage), 0o600))

	ProcessPendingSynthIDRecords(context.Background(), 10, nil)

	logs, _, err := model.GetAllLogs(model.LogTypeConsume, 0, 0, "", "", "", 0, 10, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	var other struct {
		DetectionVerdict string `json:"detection_verdict"`
		DetectionSteps   struct {
			Content string `json:"content"`
			model.ProvenanceAnswer
		} `json:"detection_steps"`
	}
	require.NoError(t, common.UnmarshalJsonStr(logs[0].Other, &other))
	assert.Equal(t, model.ImageDetectionVerdictPass, other.DetectionVerdict)
	assert.Equal(t, model.ImageDetectionStepPass, other.DetectionSteps.Content)
	assert.Equal(t, model.ProvenanceAnswer{
		C2PA:      "detected",
		C2PAState: "trusted",
		Issuer:    "OpenAI OpCo, LLC",
		SynthID:   "detected",
		Model:     "gpt-image-2",
	}, other.DetectionSteps.ProvenanceAnswer)
}

// A 429 says nothing about the image: the record stays pending for a later
// run, and no further call is sent until the Retry-After wait has passed.
func TestProcessPendingSynthIDRecordsWaitsOutTheRateLimit(t *testing.T) {
	setupImageDetectionTest(t)
	stub := useProvenanceStub(t)
	stub.status = http.StatusTooManyRequests
	stub.body = `{"error":{"message":"Rate limit reached.","code":"rate_limit_exceeded"}}`
	stub.header = http.Header{"Retry-After": []string{"30"}}
	first := insertPendingSynthIDRecord(t, "req-limited-1")
	second := insertPendingSynthIDRecord(t, "req-limited-2")
	for _, rec := range []*model.ImageDetectionRecord{first, second} {
		require.NoError(t, os.WriteFile(rec.TempFilePath, []byte(pngImage), 0o600))
	}

	processed, skipped := ProcessPendingSynthIDRecords(context.Background(), 10, nil)

	assert.Equal(t, 0, processed)
	assert.Equal(t, 2, skipped)
	assert.Equal(t, 1, stub.calls)
	var got model.ImageDetectionRecord
	require.NoError(t, model.DB.First(&got, first.ID).Error)
	assert.Equal(t, model.ImageDetectionSignalPending, got.SynthIDSignal)
	assert.Equal(t, model.ImageDetectionErrorRateLimited, got.ProvenanceError)
	assert.Contains(t, got.ProvenanceErrorDetail, "Rate limit reached.")
	assert.FileExists(t, first.TempFilePath)
}

func TestRunC2PACheckReportsAMissingTool(t *testing.T) {
	signal, reason := runC2PACheck(context.Background(), []byte(pngImage),
		filepath.Join(t.TempDir(), "missing-c2patool.exe"), 5*time.Second)

	assert.Equal(t, model.ImageDetectionSignalError, signal)
	assert.Equal(t, model.ImageDetectionErrorToolUnavailable, reason)
}

// A manifest only stands in for the official check when it holds up the way
// the Content Provenance API requires: issued by OpenAI, recording an AI
// generation action and validated as trusted.
func TestRunC2PACheckCountsOnlyTrustedOpenAICredentials(t *testing.T) {
	tests := []struct {
		name       string
		report     string
		wantSignal string
		wantReason string
	}{
		{
			name:       "trusted OpenAI generation",
			report:     c2paReport(openAIIssuer, aiGenerated, "Trusted"),
			wantSignal: model.ImageDetectionSignalDetected,
		},
		{
			name:       "image changed after signing",
			report:     c2paReport(openAIIssuer, aiGenerated, "Invalid"),
			wantSignal: model.ImageDetectionSignalNone,
			wantReason: model.ImageDetectionReasonInvalidCredentials,
		},
		{
			name:       "signed by another issuer",
			report:     c2paReport("Google LLC", aiGenerated, "Trusted"),
			wantSignal: model.ImageDetectionSignalNone,
			wantReason: model.ImageDetectionReasonOtherIssuer,
		},
		{
			name:       "no AI generation recorded",
			report:     c2paReport(openAIIssuer, "http://cv.iptc.org/newscodes/digitalsourcetype/digitalCapture", "Trusted"),
			wantSignal: model.ImageDetectionSignalNone,
			wantReason: model.ImageDetectionReasonNoAIAction,
		},
		{
			name:       "signature not traced to a trusted certificate",
			report:     c2paReport(openAIIssuer, aiGenerated, "Valid"),
			wantSignal: model.ImageDetectionSignalNone,
			wantReason: model.ImageDetectionReasonUntrustedCredentials,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(fakeC2PAToolEnv, tc.report)

			signal, reason := runC2PACheck(context.Background(), []byte(pngImage), os.Args[0], 30*time.Second)

			assert.Equal(t, tc.wantSignal, signal)
			assert.Equal(t, tc.wantReason, reason)
		})
	}
}

func TestProcessPendingSynthIDRecordsKeepsTheImageWhenTheCheckCannotRun(t *testing.T) {
	setupImageDetectionTest(t)
	rec := insertPendingSynthIDRecord(t, "req-no-key")

	ProcessPendingSynthIDRecords(context.Background(), 10, nil)

	var got model.ImageDetectionRecord
	require.NoError(t, model.DB.First(&got, rec.ID).Error)
	assert.Equal(t, model.ImageDetectionSignalError, got.SynthIDSignal)
	assert.Equal(t, model.ImageDetectionErrorNoAPIKey, got.ProvenanceError)
	assert.Equal(t, rec.TempFilePath, got.TempFilePath)
	assert.FileExists(t, rec.TempFilePath)
}

func insertRetainedImageRecord(t *testing.T, requestID string, updatedAt int64) *model.ImageDetectionRecord {
	t.Helper()
	imgPath := filepath.Join(t.TempDir(), requestID+".png")
	require.NoError(t, os.WriteFile(imgPath, []byte(pngImage), 0o600))
	rec := &model.ImageDetectionRecord{
		RequestID:       requestID,
		C2PASignal:      model.ImageDetectionSignalNone,
		SynthIDSignal:   model.ImageDetectionSignalError,
		ProvenanceError: model.ImageDetectionErrorNoAPIKey,
		TempFilePath:    imgPath,
		CreatedAt:       updatedAt,
		UpdatedAt:       updatedAt,
	}
	require.NoError(t, model.InsertImageDetectionRecord(rec))
	return rec
}

func TestPruneRetainedDetectionImagesKeepsOnlyTheNewestRecentImages(t *testing.T) {
	setupImageDetectionTest(t)
	now := time.Now().Unix()
	tooOld := insertRetainedImageRecord(t, "req-too-old", now-4*24*3600)
	overCount := insertRetainedImageRecord(t, "req-over-count", now)
	kept := []*model.ImageDetectionRecord{
		insertRetainedImageRecord(t, "req-newer", now),
		insertRetainedImageRecord(t, "req-newest", now),
	}

	pruneRetainedDetectionImages(2, 72*time.Hour)

	for _, rec := range []*model.ImageDetectionRecord{tooOld, overCount} {
		var got model.ImageDetectionRecord
		require.NoError(t, model.DB.First(&got, rec.ID).Error)
		assert.Empty(t, got.TempFilePath, rec.RequestID)
		assert.NoFileExists(t, rec.TempFilePath)
	}
	for _, rec := range kept {
		var got model.ImageDetectionRecord
		require.NoError(t, model.DB.First(&got, rec.ID).Error)
		assert.Equal(t, rec.TempFilePath, got.TempFilePath, rec.RequestID)
		assert.FileExists(t, rec.TempFilePath)
	}
}

func TestGetRetainedDetectionImageReturnsTheLatestKeptImage(t *testing.T) {
	setupImageDetectionTest(t)
	insertRetainedImageRecord(t, "req-image", time.Now().Unix())
	latest := insertRetainedImageRecord(t, "req-image", time.Now().Unix())

	rec, err := model.GetRetainedDetectionImage("req-image")

	require.NoError(t, err)
	assert.Equal(t, latest.ID, rec.ID)
	assert.Equal(t, latest.TempFilePath, rec.TempFilePath)
}

func TestGetUserLogsHidesImageDetection(t *testing.T) {
	setupImageDetectionTest(t)
	useLogTable(t)
	require.NoError(t, model.DB.Create(&model.Log{
		UserId:    7,
		Type:      model.LogTypeConsume,
		RequestId: "req-user",
		ModelName: "gpt-image-1",
		CreatedAt: 1793188800,
	}).Error)
	insertRetainedImageRecord(t, "req-user", 1793188800)

	logs, _, err := model.GetUserLogs(7, model.LogTypeConsume, 0, 0, "", "", 0, 10, "", "", "")

	require.NoError(t, err)
	require.Len(t, logs, 1)
	assert.NotContains(t, logs[0].Other, "detection_")
}
