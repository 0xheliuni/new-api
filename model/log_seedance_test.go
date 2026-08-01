package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func clearLogsAndTasks(t *testing.T) {
	t.Helper()
	require.NoError(t, LOG_DB.Exec("DELETE FROM logs").Error)
	require.NoError(t, DB.Exec("DELETE FROM tasks").Error)
	t.Cleanup(func() {
		LOG_DB.Exec("DELETE FROM logs")
		DB.Exec("DELETE FROM tasks")
	})
}

// seedance 的 settle/refund 行从分页列表隐藏(count 同步)；预扣行保留；
// 非 seedance 的任务行不受影响。
func TestGetLogs_HidesSeedanceSettleRefundRows(t *testing.T) {
	clearLogsAndTasks(t)
	mk := func(typ int, model, other string) {
		require.NoError(t, LOG_DB.Create(&Log{
			UserId: 1, Username: "u", Type: typ, ModelName: model,
			CreatedAt: 1000, Quota: 100, Other: other,
		}).Error)
	}
	mk(LogTypeConsume, "doubao-seedance-1-0", `{"is_task":true,"billing_stage":"pre_consume","task_id":"t1"}`)
	mk(LogTypeConsume, "doubao-seedance-1-0", `{"billing_stage":"settle","task_id":"t1","pre_consumed_quota":100,"actual_quota":150}`)
	mk(LogTypeRefund, "doubao-seedance-1-0", `{"billing_stage":"refund","task_id":"t1"}`)
	// 非 seedance 任务的 settle 行必须保留
	mk(LogTypeConsume, "grok-video-1", `{"billing_stage":"settle","task_id":"g1"}`)
	// 普通聊天日志保留
	mk(LogTypeConsume, "gpt-4o", ``)

	logs, total, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 3, total, "count 必须同步排除")
	models := map[string]int{}
	for _, l := range logs {
		models[l.ModelName]++
	}
	assert.Equal(t, 1, models["doubao-seedance-1-0"], "只剩预扣行")
	assert.Equal(t, 1, models["grok-video-1"])
	assert.Equal(t, 1, models["gpt-4o"])

	// self 路径同规则
	ulogs, utotal, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 100, "", "", "")
	require.NoError(t, err)
	assert.EqualValues(t, 3, utotal)
	assert.Len(t, ulogs, 3)
}

// 成功任务：状态/进度/net 金额/tokens/档位/秒数/倍率齐备；admin 可见上游 ID。
func TestEnrichSeedance_SuccessWithSettle(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID: "vt1", UserId: 1, Status: TaskStatusSuccess, Progress: "100%",
		Quota: 150, PrivateData: TaskPrivateData{UpstreamTaskID: "up-999", RequestId: "req-1"},
		Data: []byte(`{"model":"doubao-seedance-1-0","duration":5,"usage":{"completion_tokens":123}}`),
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 1000,
		Quota: 100, RequestId: "req-1",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt1","video_resolution_tier":"720p","video_has_input":true,"group_ratio":1,"user_group_ratio":0.5}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{ // settle 补扣 50
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 1100,
		Quota: 50, RequestId: "req-1",
		Other: `{"billing_stage":"settle","task_id":"vt1","pre_consumed_quota":100,"actual_quota":150,"video_tokens":123}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1, "settle 行已隐藏")
	ti := logs[0].TaskInfo
	require.NotNil(t, ti)
	assert.Equal(t, "SUCCESS", ti.Status)
	assert.Equal(t, "100%", ti.Progress)
	assert.Equal(t, 100, ti.PreQuota)
	assert.Equal(t, 150, ti.FinalQuota, "预扣100+补扣50")
	assert.Equal(t, 123, ti.OutputTokens)
	assert.Equal(t, "720p", ti.ResolutionTier)
	assert.Equal(t, 5, ti.DurationS)
	assert.True(t, ti.HasInput)
	assert.Equal(t, 0.5, ti.EffectiveRatio)
	assert.True(t, ti.IsUserRatio)
	assert.Equal(t, "vt1", ti.TaskId)
	assert.Equal(t, "up-999", ti.UpstreamTaskId, "admin 路径可见上游 ID")
}

// 失败全额退款：FinalQuota=0；self 路径不给上游 ID；失败原因来自 task。
func TestEnrichSeedance_FailureRefundAndSelfPath(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID: "vt2", UserId: 1, Status: TaskStatusFailure, Progress: "100%",
		FailReason: "content policy", Quota: 100,
		PrivateData: TaskPrivateData{UpstreamTaskID: "up-2", RequestId: "req-2"},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "dreamina-seedance-2-0", CreatedAt: 1000,
		Quota: 100, RequestId: "req-2",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt2","group_ratio":0.8}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeRefund, ModelName: "dreamina-seedance-2-0", CreatedAt: 1100,
		Quota: 100, RequestId: "req-2",
		Other: `{"billing_stage":"refund","task_id":"vt2","reason":"content policy"}`,
	}).Error)

	logs, _, err := GetUserLogs(1, LogTypeUnknown, 0, 0, "", "", 0, 100, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 1)
	ti := logs[0].TaskInfo
	require.NotNil(t, ti)
	assert.Equal(t, "FAILURE", ti.Status)
	assert.Equal(t, 0, ti.FinalQuota)
	assert.Equal(t, "content policy", ti.FailReason)
	assert.Empty(t, ti.UpstreamTaskId, "self 路径不暴露上游 ID")
	assert.Equal(t, 0.8, ti.EffectiveRatio)
	assert.False(t, ti.IsUserRatio)
}

// 进行中（无兄弟行）与 task 被清理（兄弟推断）两种兜底。
func TestEnrichSeedance_InProgressAndOrphan(t *testing.T) {
	clearLogsAndTasks(t)
	require.NoError(t, DB.Create(&Task{
		TaskID: "vt3", UserId: 1, Status: TaskStatusInProgress, Progress: "30%", Quota: 100,
		PrivateData: TaskPrivateData{RequestId: "req-3"},
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 1000,
		Quota: 100, RequestId: "req-3",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"vt3","group_ratio":1}`,
	}).Error)
	// orphan: 无 task 行，但有 settle 兄弟 → SUCCESS
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 2000,
		Quota: 100, RequestId: "req-4",
		Other: `{"is_task":true,"billing_stage":"pre_consume","task_id":"gone","group_ratio":1}`,
	}).Error)
	require.NoError(t, LOG_DB.Create(&Log{
		UserId: 1, Type: LogTypeConsume, ModelName: "doubao-seedance-1-0", CreatedAt: 2100,
		Quota: 20, RequestId: "req-4",
		Other: `{"billing_stage":"settle","task_id":"gone","pre_consumed_quota":100,"actual_quota":120}`,
	}).Error)

	logs, _, err := GetAllLogs(LogTypeUnknown, 0, 0, "", "", "", 0, 100, 0, "", "", "")
	require.NoError(t, err)
	require.Len(t, logs, 2)
	byReq := map[string]*Log{}
	for _, l := range logs {
		byReq[l.RequestId] = l
	}
	require.NotNil(t, byReq["req-3"].TaskInfo)
	assert.Equal(t, "IN_PROGRESS", byReq["req-3"].TaskInfo.Status)
	assert.Equal(t, "30%", byReq["req-3"].TaskInfo.Progress)
	require.NotNil(t, byReq["req-4"].TaskInfo)
	assert.Equal(t, "SUCCESS", byReq["req-4"].TaskInfo.Status, "task 缺失时由 settle 兄弟推断")
	assert.Equal(t, 120, byReq["req-4"].TaskInfo.FinalQuota)
}
