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

import type { ChannelFormValues } from '../../../lib/channel-form'
import {
  hasAssetLibrary,
  hasSeedanceAssetUpload,
  hasUpstreamTaskIdOption,
} from '../../../lib/video-channel-extensions'
import { AssetLibraryFields } from './asset-library-fields'
import { SeedanceThirdPartyFields } from './seedance-third-party-fields'
import { UpstreamTaskIdField } from './upstream-task-id-field'

type VideoChannelSettingsProps = {
  form: UseFormReturn<ChannelFormValues>
  type: number
  disabled: boolean
}

/** fork 视频渠道在"连接与模型"分区里的额外配置；其他渠道类型不渲染任何内容。 */
export function VideoChannelSettings(props: VideoChannelSettingsProps) {
  return (
    <>
      {hasAssetLibrary(props.type) && (
        <AssetLibraryFields form={props.form} disabled={props.disabled} />
      )}
      {hasSeedanceAssetUpload(props.type) && (
        <SeedanceThirdPartyFields form={props.form} disabled={props.disabled} />
      )}
      {hasUpstreamTaskIdOption(props.type) && (
        <UpstreamTaskIdField form={props.form} disabled={props.disabled} />
      )}
    </>
  )
}
