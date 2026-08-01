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
