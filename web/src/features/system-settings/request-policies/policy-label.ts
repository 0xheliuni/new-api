import type { TFunction } from 'i18next'

// Labels for the decision events recorded by the backend request policy state
// (reasons, sources and health), the session behaviors, and the auto-disable
// sources reported by the channel operations summary.
export function policyLabel(t: TFunction, value: string): string {
  switch (value) {
    case 'upstream_failure':
      return t('Upstream request failed')
    case 'request_failed':
      return t('Request failed')
    case 'task_failed':
      return t('Task failed')
    case 'strict_session':
      return t('Session behavior prevents switching channels')
    case 'attempt_budget_exhausted':
      return t('Total attempt budget exhausted')
    case 'channel_error':
      return t('Channel error')
    case 'unrecognized_status':
      return t('Unrecognized status code')
    case 'task_accepted':
      return t('The task was accepted; submission cannot be replayed')
    case 'pinned_channel':
      return t('Fixed channel constraint')
    case 'local_rejection':
      return t('Request rejected locally')
    case 'non_retryable_error':
    case 'system_retry_exclusion':
      return t('System retry exclusion')
    case 'status_not_retryable':
      return t('Status is outside the retry rules')
    case 'retry_status_matched':
      return t('Status matches the retry rules')
    case 'session_rule_matched':
      return t('Session rule matched')
    case 'channel_selected':
      return t('Channel selected')
    case 'request_completed':
      return t('Request completed')
    case 'stream_not_successful':
      return t('Stream did not complete successfully')
    case 'channel':
      return t('Channel')
    case 'global':
      return t('Global default')
    case 'global_and_channel':
      return t('Global and channel settings')
    case 'session_rule':
      return t('Session rule')
    case 'system':
      return t('System constraint')
    case 'routing':
      return t('Channel selection')
    case 'upstream':
      return t('Upstream response')
    case 'transport':
      return t('Upstream connection')
    case 'local':
      return t('Local check')
    case 'channel_constraint':
      return t('Fixed channel constraint')
    case 'unchanged':
      return t('Unchanged')
    case 'channel_disable_requested':
      return t('Channel disable requested')
    case 'key_disable_requested':
      return t('Current key disable requested')
    case 'priority_limit_reached':
      return t('Priority level fully tried; moved to the next priority')
    case 'cooldown_fallback':
      return t('All channels are cooling down; picked the one that recovers first')
    case 'tried_channel_reused':
      return t('No untried channels left; reused a tried channel')
    case 'session_channel_cooling':
      return t(
        'The session channel is cooling down; switched channels for this request'
      )
    case 'cooldown_started':
      return t('Cooldown started')
    case 'already_cooling':
      return t('Already cooling down; not counted')
    case 'cooldown_disable_requested':
      return t('Too many cooldowns in a row; channel disable requested')
    case 'cooldown_upstream_wide':
      return t(
        'Most channels at this priority are cooling down; kept the channel enabled'
      )
    case 'cooldown':
      return t('Channel cooldown')
    case 'off':
    case '':
      return t('Do not keep sessions')
    case 'prefer':
      return t('Prefer the original channel, allow switching')
    case 'strict':
      return t('Require the original channel')
    default:
      return value
  }
}
