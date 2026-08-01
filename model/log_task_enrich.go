package model

import (
	"strconv"
	"strings"

	"github.com/QuantumNous/new-api/common"
)

// LogTaskInfo 是 seedance 视频任务预扣行的查询时增强载荷（gorm:"-"，不落库）。
// 前端据此渲染完成状态列、三态费用与详情区块；字段命名与两个前端的
// task_info 消费方保持一致。
type LogTaskInfo struct {
	Status         string  `json:"status"`
	Progress       string  `json:"progress,omitempty"`
	FailReason     string  `json:"fail_reason,omitempty"`
	TaskId         string  `json:"task_id,omitempty"`
	UpstreamTaskId string  `json:"upstream_task_id,omitempty"`
	PreQuota       int     `json:"pre_quota"`
	FinalQuota     int     `json:"final_quota"`
	OutputTokens   int     `json:"output_tokens,omitempty"`
	ResolutionTier string  `json:"resolution_tier,omitempty"`
	DurationS      int     `json:"duration_s,omitempty"`
	HasInput       bool    `json:"has_input"`
	EffectiveRatio float64 `json:"effective_ratio,omitempty"`
	IsUserRatio    bool    `json:"is_user_ratio,omitempty"`
}

// EnrichSeedanceTaskLogs 是 enrichSeedanceTaskLogs 的导出入口，供原始日志导出
// （controller）在流式批次上复用同一套单行化增强。
func EnrichSeedanceTaskLogs(logs []*Log, includeUpstreamId bool) {
	enrichSeedanceTaskLogs(logs, includeUpstreamId)
}

// enrichSeedanceTaskLogs 对页内 seedance 预扣行挂 task_info：
// 两次批量查询（tasks by task_id、兄弟日志 by request_id），无逐行 N+1。
// includeUpstreamId=false（self 路径）不暴露上游任务 ID。
func enrichSeedanceTaskLogs(logs []*Log, includeUpstreamId bool) {
	type candidate struct {
		log   *Log
		other map[string]interface{}
	}
	var cands []candidate
	taskIds := make([]string, 0)
	reqIds := make([]string, 0)
	for _, l := range logs {
		if l.Type != LogTypeConsume || !strings.Contains(l.ModelName, "seedance") {
			continue
		}
		if !strings.Contains(l.Other, `"billing_stage":"pre_consume"`) {
			continue
		}
		m, err := common.StrToMap(l.Other)
		if err != nil || m == nil {
			continue
		}
		cands = append(cands, candidate{log: l, other: m})
		if tid, _ := m["task_id"].(string); tid != "" {
			taskIds = append(taskIds, tid)
		}
		if l.RequestId != "" {
			reqIds = append(reqIds, l.RequestId)
		}
	}
	if len(cands) == 0 {
		return
	}

	taskById := make(map[string]*Task)
	if len(taskIds) > 0 {
		var tasks []*Task
		if err := DB.Where("task_id IN ?", taskIds).Find(&tasks).Error; err == nil {
			for _, tk := range tasks {
				taskById[tk.TaskID] = tk
			}
		}
	}

	// 兄弟结算/退款行（分页列表里被隐藏，这里按 request_id 补查取金额与原因）
	type sibling struct {
		typ   int
		quota int
		other map[string]interface{}
	}
	sibByReq := make(map[string][]sibling)
	if len(reqIds) > 0 {
		var sibs []*Log
		if err := LOG_DB.Where("request_id IN ? AND type IN ?", reqIds, []int{LogTypeConsume, LogTypeRefund}).Find(&sibs).Error; err == nil {
			for _, s := range sibs {
				if !strings.Contains(s.ModelName, "seedance") {
					continue
				}
				if !strings.Contains(s.Other, `"billing_stage":"settle"`) && !strings.Contains(s.Other, `"billing_stage":"refund"`) {
					continue
				}
				m, err := common.StrToMap(s.Other)
				if err != nil {
					m = nil
				}
				sibByReq[s.RequestId] = append(sibByReq[s.RequestId], sibling{typ: s.Type, quota: s.Quota, other: m})
			}
		}
	}

	num := func(m map[string]interface{}, key string) float64 {
		if m == nil {
			return 0
		}
		if v, ok := m[key].(float64); ok {
			return v
		}
		return 0
	}

	for _, c := range cands {
		ti := &LogTaskInfo{Status: string(TaskStatusUnknown), PreQuota: c.log.Quota, FinalQuota: c.log.Quota}
		ti.TaskId, _ = c.other["task_id"].(string)
		ti.ResolutionTier, _ = c.other["video_resolution_tier"].(string)
		ti.HasInput, _ = c.other["video_has_input"].(bool)
		if ug := num(c.other, "user_group_ratio"); ug > 0 && ug != -1 {
			ti.EffectiveRatio, ti.IsUserRatio = ug, true
		} else if gr := num(c.other, "group_ratio"); gr > 0 {
			ti.EffectiveRatio = gr
		}

		hasSettle, hasRefund := false, false
		for _, s := range sibByReq[c.log.RequestId] {
			if s.typ == LogTypeRefund {
				hasRefund = true
				ti.FinalQuota -= s.quota
			} else {
				hasSettle = true
				ti.FinalQuota += s.quota
			}
			if vt := int(num(s.other, "video_tokens")); vt > 0 {
				ti.OutputTokens = vt
			}
			if ti.FailReason == "" {
				ti.FailReason, _ = s.other["reason"].(string)
			}
		}
		if ti.FinalQuota < 0 {
			ti.FinalQuota = 0
		}

		if tk := taskById[ti.TaskId]; tk != nil {
			ti.Status = string(tk.Status)
			ti.Progress = tk.Progress
			if tk.FailReason != "" && tk.Status == TaskStatusFailure {
				ti.FailReason = tk.FailReason
			}
			if includeUpstreamId {
				ti.UpstreamTaskId = tk.GetUpstreamTaskID()
			}
			dur, outTok := parseTaskVideoMeta(tk.Data)
			if dur > 0 {
				ti.DurationS = dur
			}
			if ti.OutputTokens == 0 && outTok > 0 {
				ti.OutputTokens = outTok
			}
		} else {
			// task 已被清理：由兄弟行推断终态
			switch {
			case hasRefund:
				ti.Status = string(TaskStatusFailure)
			case hasSettle:
				ti.Status = string(TaskStatusSuccess)
			}
		}
		c.log.TaskInfo = ti
	}
}

// parseTaskVideoMeta 从 task.Data(脱敏后的上游最终响应 JSON)尽力解析生成秒数与
// 输出 tokens。键名因 adaptor 而异，扫描顶层与一层嵌套的常见键；解析不到返回 0。
func parseTaskVideoMeta(data []byte) (durationS int, outputTokens int) {
	if len(data) == 0 {
		return 0, 0
	}
	var m map[string]interface{}
	if err := common.Unmarshal(data, &m); err != nil {
		return 0, 0
	}
	pickInt := func(m map[string]interface{}, keys ...string) int {
		for _, k := range keys {
			switch v := m[k].(type) {
			case float64:
				if v > 0 {
					return int(v)
				}
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && n > 0 {
					return n
				}
			}
		}
		return 0
	}
	scan := func(m map[string]interface{}) {
		if durationS == 0 {
			durationS = pickInt(m, "duration", "seconds", "duration_seconds", "video_duration")
		}
		if outputTokens == 0 {
			outputTokens = pickInt(m, "completion_tokens", "total_tokens")
		}
	}
	scan(m)
	for _, v := range m {
		if sub, ok := v.(map[string]interface{}); ok {
			scan(sub)
		}
	}
	return durationS, outputTokens
}
