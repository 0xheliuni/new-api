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
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import type { ChannelFormValues } from '../../../lib/channel-form'
import { VideoChannelBlock } from './video-channel-block'
import {
  VideoChannelSwitchField,
  VideoChannelTextField,
} from './video-channel-fields'

type AssetLibraryFieldsProps = {
  form: UseFormReturn<ChannelFormValues>
  disabled: boolean
}

/** 火山方舟 / 豆包视频的素材库预上传。官方 BytePlus 用 AK/SK 签名；Cloudwise 复用渠道地址和密钥。 */
export function AssetLibraryFields(props: AssetLibraryFieldsProps) {
  const { t } = useTranslation()
  const enabled = props.form.watch('byteplus_asset_enabled') === true
  const official = props.form.watch('asset_provider') !== 'cloudwise'
  const providerItems = [
    { value: 'byteplus', label: t('Official (BytePlus)') },
    { value: 'cloudwise', label: t('Third-party (Cloudwise)') },
  ]

  return (
    <VideoChannelBlock title={t('Asset library pre-upload')}>
      <VideoChannelSwitchField
        form={props.form}
        name='byteplus_asset_enabled'
        label={t('Enable asset pre-upload')}
        description={t(
          'Before submitting, upload reference media to the asset library and replace it with asset://id. Avoids real-face / content pre-filter interception.'
        )}
        disabled={props.disabled}
      />
      {enabled && (
        <>
          <FormField
            control={props.form.control}
            name='asset_provider'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Asset provider')}</FormLabel>
                <Select
                  disabled={props.disabled}
                  items={providerItems}
                  onValueChange={field.onChange}
                  value={field.value || 'byteplus'}
                >
                  <FormControl>
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                  </FormControl>
                  <SelectContent alignItemWithTrigger={false}>
                    <SelectGroup>
                      {providerItems.map((item) => (
                        <SelectItem key={item.value} value={item.value}>
                          {item.label}
                        </SelectItem>
                      ))}
                    </SelectGroup>
                  </SelectContent>
                </Select>
                <FormMessage />
              </FormItem>
            )}
          />
          {official ? (
            <>
              <VideoChannelTextField
                form={props.form}
                name='byteplus_access_key'
                label={t('Access Key (AK)')}
                placeholder={t('BytePlus AccessKey for asset library signing')}
                secret
                disabled={props.disabled}
              />
              <VideoChannelTextField
                form={props.form}
                name='byteplus_secret_key'
                label={t('Secret Key (SK)')}
                placeholder={t('BytePlus SecretKey for asset library signing')}
                secret
                disabled={props.disabled}
              />
            </>
          ) : (
            <p className='text-muted-foreground text-sm'>
              {t(
                'Third-party asset library reuses the channel API address and key.'
              )}
            </p>
          )}
          <VideoChannelTextField
            form={props.form}
            name='byteplus_asset_group_id'
            label={t('Asset Group ID')}
            placeholder={t('e.g., group-20260318033332-xxxxx')}
            description={t(
              'Pre-create the asset group in the BytePlus console (sign the authorization letter on first use).'
            )}
            disabled={props.disabled}
          />
          {official && (
            <>
              <VideoChannelTextField
                form={props.form}
                name='byteplus_project_name'
                label={t('Project Name')}
                placeholder='default'
                description={t(
                  'Assets and inference endpoint must be in the same project.'
                )}
                disabled={props.disabled}
              />
              <VideoChannelTextField
                form={props.form}
                name='byteplus_region'
                label={t('Region')}
                placeholder='ap-southeast-1'
                disabled={props.disabled}
              />
            </>
          )}
          <VideoChannelSwitchField
            form={props.form}
            name='byteplus_moderation_skip'
            label={t('Skip content pre-filter')}
            description={t(
              'Send Moderation.Strategy=Skip on upload. Celebrity / copyright IP is still intercepted. Ignored for cn-* regions, which reject this parameter.'
            )}
            disabled={props.disabled}
          />
        </>
      )}
    </VideoChannelBlock>
  )
}
