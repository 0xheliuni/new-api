package controller

import (
	"context"
	"time"

	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// intContains reports whether slice contains val.
func intContains(slice []int, val int) bool {
	for _, v := range slice {
		if v == val {
			return true
		}
	}
	return false
}

// imageDetectionHandler runs scheduled image detection sampling as a system
// task. It implements CronScheduledSystemTaskHandler so the scheduler uses the
// configured cron expression instead of a fixed interval.
type imageDetectionHandler struct{}

func (imageDetectionHandler) Type() string { return model.SystemTaskTypeImageDetection }

func (imageDetectionHandler) Enabled() bool {
	return operation_setting.GetImageDetectionSetting().ScheduledEnabled
}

// Interval is the fallback used only when the cron expression is absent or
// unparseable; the scheduler prefers NextRunAfter.
func (imageDetectionHandler) Interval() time.Duration { return 2 * time.Hour }

func (imageDetectionHandler) CronExpression() string {
	return operation_setting.GetImageDetectionSetting().ScheduledCron
}

func (h imageDetectionHandler) NextRunAfter(ref time.Time) time.Time {
	return service.ParseAndNext(h.CronExpression(), ref)
}

func (imageDetectionHandler) NewPayload() any { return imageDetectionTaskPayload{} }

type imageDetectionTaskPayload struct {
	// ChannelIDs restricts the run to specific channels when non-empty.
	// An empty slice means "all channels in scope".
	ChannelIDs []int `json:"channel_ids,omitempty"`
}

func (imageDetectionHandler) Run(ctx context.Context, task *model.SystemTask, runnerID string) {
	payload := imageDetectionTaskPayload{}
	if err := task.DecodePayload(&payload); err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	result, err := runImageDetectionTask(ctx, payload)
	if err != nil {
		finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusFailed, nil, err)
		return
	}
	finishSystemTaskHandler(task, runnerID, model.SystemTaskStatusSucceeded, result, nil)
}

type imageDetectionTaskResult struct {
	Scanned   int `json:"scanned"`
	Processed int `json:"processed"`
	Skipped   int `json:"skipped"`
}

// runImageDetectionTask drains the pending SynthID queue for a given channel
// scope. With passive retention, scheduled and targeted runs only process
// records that the global trigger already created and retained; they do not
// re-scan historical tasks (those have no retained image bytes).
func runImageDetectionTask(ctx context.Context, payload imageDetectionTaskPayload) (imageDetectionTaskResult, error) {
	s := operation_setting.GetImageDetectionSetting()
	result := imageDetectionTaskResult{}

	result.Scanned = 0 // no historical re-scan in passive mode

	// Drain pending SynthID queue, optionally filtered to payload.ChannelIDs.
	processed, skipped := service.ProcessPendingSynthIDRecords(ctx, s.ScheduledBatchLimit, payload.ChannelIDs)
	result.Processed += processed
	result.Skipped += skipped

	return result, nil
}
