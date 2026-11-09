/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import type { ChannelFormValues } from '../../../lib/channel-form'
import { VideoChannelBlock } from './video-channel-block'
import { VideoChannelSwitchField } from './video-channel-fields'

type UpstreamTaskIdFieldProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled: boolean
}

/** 在视频响应里额外输出上游任务 ID；键名 expose_upstream_task_id 已落库，不能改。 */
export function UpstreamTaskIdField(props: UpstreamTaskIdFieldProps) {
  const { t } = useTranslation()

  return (
    <VideoChannelBlock title={t('Upstream Task ID Passthrough')}>
      <VideoChannelSwitchField
        form={props.form}
        name='expose_upstream_task_id'
        label={t('Expose upstream task id to clients')}
        description={t(
          'Adds an upstream_task_id field (e.g. cgt-...) to video responses. The id / task_id fields stay unchanged, so clients keep polling with the public id. Note: this reveals the upstream provider.'
        )}
        disabled={props.disabled}
      />
    </VideoChannelBlock>
  )
}
