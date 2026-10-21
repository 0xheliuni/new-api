package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetLogs_HidesWan3SettleRefundRows verifies that wan3.0-video settle/refund
// rows are excluded from paged usage-log listings, and that the pre-consume row
// is retained. Non-wan3.0 settle rows must not be affected.
func TestGetLogs_HidesWan3SettleRefundRows(t *testing.T) {
	clearLogsAndTasks(t)
	mk := func(typ int, model, other string) {
		require.NoError(t, LOG_DB.Create(&Log{
			UserId:    1,
			Username:  "u",
			Type:      typ,
			ModelName: model,
			CreatedAt: 1000,
			Quota:     100,
			Other:     other,
		}).Error)
	}
	// wan3.0-video: pre_consume stays, settle+refund hidden
	mk(LogTypeConsume, "wan3.0-video", `{"is_task":true,"billing_stage":"pre_consume","task_id":"wt1"}`)
	mk(LogTypeConsume, "wan3.0-video", `{"billing_stage":"settle","task_id":"wt1","pre_consumed_quota":100,"actual_quota":150}`)
	mk(LogTypeRefund, "wan3.0-video", `{"billing_stage":"refund","task_id":"wt1"}`)
	// wan3.0-video-prime: same expectation
	mk(LogTypeConsume, "wan3.0-video-prime", `{"is_task":true,"billing_stage":"pre_consume","task_id":"wt2"}`)
	mk(LogTypeConsume, "wan3.0-video-prime", `{"billing_stage":"settle","task_id":"wt2","pre_consumed_quota":100,"actual_quota":120}`)
	// non-async model settle row must be preserved
	mk(LogTypeConsume, "gpt-4o", `{"billing_stage":"settle","task_id":"g1"}`)

	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	// 2 wan3.0 pre_consume + 1 gpt-4o settle = 3
	assert.EqualValues(t, 3, total, "wan3.0 settle/refund rows must be hidden, count must be 3")
	models := map[string]int{}
	for _, l := range logs {
		models[l.ModelName]++
	}
	assert.Equal(t, 1, models["wan3.0-video"], "only pre_consume row should remain for wan3.0-video")
	assert.Equal(t, 1, models["wan3.0-video-prime"], "only pre_consume row should remain for wan3.0-video-prime")
	assert.Equal(t, 1, models["gpt-4o"], "non-async settle row must be preserved")

	// self path: same rule
	ulogs, utotal, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 100, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 3, utotal)
	assert.Len(t, ulogs, 3)
}

// TestEnrichWan3_SuccessWithSettle verifies that a wan3.0-video pre_consume row
// receives TaskInfo with correct status, final quota, resolution, ratio and duration.
func TestEnrichWan3_SuccessWithSettle(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID:   "wt10",
		UserId:   1,
		Status:   TaskStatusSuccess,
		Progress: "100%",
		Quota:    150,
		PrivateData: TaskPrivateData{
			UpstreamTaskID: "up-w10",
			RequestId:      "req-w10",
		},
		Data: []byte(`{"usage":{"output_video_duration":8}}`),
		Properties: Properties{
			Input: `{"resolution":"720P","ratio":"16:9","duration":8}`,
		},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		Type:      LogTypeConsume,
		ModelName: "wan3.0-video",
		CreatedAt: 1000,
		Quota:     100,
		RequestId: "req-w10",
		Other:     `{"is_task":true,"billing_stage":"pre_consume","task_id":"wt10","video_resolution_tier":"720P","video_has_input":false,"group_ratio":1}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		Type:      LogTypeConsume,
		ModelName: "wan3.0-video",
		CreatedAt: 1100,
		Quota:     50,
		RequestId: "req-w10",
		Other:     `{"billing_stage":"settle","task_id":"wt10","pre_consumed_quota":100,"actual_quota":150}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1, "settle row must be hidden")
	ti := logs[0].TaskInfo
	require.NotNil(t, ti, "pre_consume row must carry TaskInfo")
	assert.Equal(t, "SUCCESS", ti.Status)
	assert.Equal(t, "100%", ti.Progress)
	assert.Equal(t, 100, ti.PreQuota)
	assert.Equal(t, 150, ti.FinalQuota, "pre_consumed(100) + settle(50) = 150")
	assert.Equal(t, "720P", ti.Resolution)
	assert.Equal(t, "16:9", ti.Ratio)
	assert.Equal(t, 8, ti.DurationS)
	assert.Equal(t, "up-w10", ti.UpstreamTaskId, "admin path exposes upstream task ID")
}

// TestEnrichWan3_DurationNegativeOne verifies that when duration=-1 is in the
// request, DurationS is resolved from upstream usage.output_video_duration, not
// stored as -1.
func TestEnrichWan3_DurationNegativeOne(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID:   "wt11",
		UserId:   1,
		Status:   TaskStatusSuccess,
		Progress: "100%",
		Quota:    200,
		PrivateData: TaskPrivateData{
			RequestId: "req-w11",
		},
		Data: []byte(`{"usage":{"output_video_duration":12}}`),
		Properties: Properties{
			Input: `{"duration":-1,"resolution":"1080P"}`,
		},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		Type:      LogTypeConsume,
		ModelName: "wan3.0-video-prime",
		CreatedAt: 1000,
		Quota:     200,
		RequestId: "req-w11",
		Other:     `{"is_task":true,"billing_stage":"pre_consume","task_id":"wt11","group_ratio":1}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	ti := logs[0].TaskInfo
	require.NotNil(t, ti)
	// parseTaskRequestParams returns 0 for duration=-1 (<=0 guard)
	// parseTaskVideoMeta falls back from task.Data output_video_duration=12
	assert.Equal(t, 12, ti.DurationS, "duration=-1 must resolve to upstream output_video_duration via parseTaskVideoMeta")
	assert.Equal(t, "1080P", ti.Resolution)
}

// TestEnrichWan3_FailureRefund verifies failure status and full quota refund.
func TestEnrichWan3_FailureRefund(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID:     "wt12",
		UserId:     1,
		Status:     TaskStatusFailure,
		Progress:   "100%",
		FailReason: "content policy violation",
		Quota:      100,
		PrivateData: TaskPrivateData{
			RequestId: "req-w12",
		},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		Type:      LogTypeConsume,
		ModelName: "wan3.0-video",
		CreatedAt: 1000,
		Quota:     100,
		RequestId: "req-w12",
		Other:     `{"is_task":true,"billing_stage":"pre_consume","task_id":"wt12","group_ratio":1}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId:    1,
		Type:      LogTypeRefund,
		ModelName: "wan3.0-video",
		CreatedAt: 1100,
		Quota:     100,
		RequestId: "req-w12",
		Other:     `{"billing_stage":"refund","task_id":"wt12"}`,
		Content:   "content policy violation",
	}).Error)

	logs, _, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 100, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1, "refund row must be hidden")
	ti := logs[0].TaskInfo
	require.NotNil(t, ti)
	assert.Equal(t, "FAILURE", ti.Status)
	assert.Equal(t, 0, ti.FinalQuota, "pre_consumed(100) - refund(100) = 0")
	assert.NotEmpty(t, ti.FailReason)
	assert.Empty(t, ti.UpstreamTaskId, "self path must not expose upstream task ID")
}
