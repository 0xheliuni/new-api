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

import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'

import type { ChannelFormValues } from '../../../lib/channel-form'
import { VideoChannelBlock } from './video-channel-block'
import { VideoChannelTextField } from './video-channel-fields'

type VolcPassthroughFieldsProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled: boolean
}

/**
 * 字节火山透传的控制面签名。AK/SK 只用于 ?Action= 调用的签名，
 * 与数据面的渠道密钥独立；签名地区和控制面地址留空时由后端推导。
 */
export function VolcPassthroughFields(props: VolcPassthroughFieldsProps) {
  const { t } = useTranslation()

  return (
    <VideoChannelBlock title={t('Control plane signing')}>
      <VideoChannelTextField
        form={props.form}
        name='byteplus_access_key'
        label={t('Access Key (AK)')}
        placeholder={t('Volcengine / BytePlus AccessKey')}
        description={t(
          'Used only to sign control-plane (?Action=) calls. Data-plane /api/v3 calls use the channel key above.'
        )}
        disabled={props.disabled}
      />
      <VideoChannelTextField
        form={props.form}
        name='byteplus_secret_key'
        label={t('Secret Key (SK)')}
        placeholder={t('Volcengine / BytePlus SecretKey')}
        secret
        disabled={props.disabled}
      />
      <VideoChannelTextField
        form={props.form}
        name='byteplus_project_name'
        label={t('Project Name')}
        placeholder='default'
        disabled={props.disabled}
      />
      <VideoChannelTextField
        form={props.form}
        name='volc_sign_region'
        label={t('Signing Region')}
        placeholder='cn-beijing / ap-southeast-1'
        description={t(
          'Region used for AK/SK signing on control-plane (?Action=) calls. Leave blank to derive it from the channel base URL.'
        )}
        disabled={props.disabled}
      />
      <VideoChannelTextField
        form={props.form}
        name='volc_openapi_endpoint'
        label={t('Control Plane Endpoint')}
        placeholder='https://ark.cn-beijing.volcengineapi.com'
        description={t(
          'Overrides the top-level OpenAPI host. Leave blank to derive it from the signing region; only set this for a private gateway.'
        )}
        disabled={props.disabled}
      />
      <FormField
        control={props.form.control}
        name='volc_asset_quota_limit'
        render={({ field }) => (
          <FormItem>
            <FormLabel>{t('Asset Quota Limit')}</FormLabel>
            <FormControl>
              <Input
                type='number'
                min={0}
                step={1}
                name={field.name}
                ref={field.ref}
                onBlur={field.onBlur}
                value={field.value ?? ''}
                onChange={(event) => {
                  const raw = event.target.value
                  field.onChange(raw === '' ? undefined : Number(raw))
                }}
                placeholder={t('e.g., 1000')}
                disabled={props.disabled}
              />
            </FormControl>
            <FormDescription>
              {t(
                'Maximum number of assets this IAM account may hold. There is no official API to query it, so fill it in manually; leave blank or 0 for unknown / unlimited. Used only by the asset management page for reconciliation.'
              )}
            </FormDescription>
            <FormMessage />
          </FormItem>
        )}
      />
    </VideoChannelBlock>
  )
}
