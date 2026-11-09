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
import { z } from 'zod'

import {
  CHANNEL_TYPE_AIGC_VIDEO,
  CHANNEL_TYPE_SEEDANCE_3RD,
  CHANNEL_TYPE_VOLC_PASSTHROUGH,
} from '../constants'

// fork 视频渠道（火山方舟、豆包视频、OpenAI Video、Seedance 第三方、字节火山透传）
// 的表单字段、settings 读写、校验和按类型的默认值都集中在这里，上游文件只调用。
// settings 键名已经落库，后端和经典版按同样的键读取，不能改名。

// 上游没有给火山方舟、豆包视频导出类型常量。
export const CHANNEL_TYPE_VOLCENGINE = 45
export const CHANNEL_TYPE_DOUBAO_VIDEO = 54

export const ARK_CN_BASE_URL = 'https://ark.cn-beijing.volces.com'
export const ARK_AP_BASE_URL = 'https://ark.ap-southeast.bytepluses.com'
export const VIDEO_CHANNEL_BASE_URL_PRESETS: readonly string[] = [
  ARK_CN_BASE_URL,
  ARK_AP_BASE_URL,
]

const ASSET_LIBRARY_TYPES: ReadonlySet<number> = new Set([
  CHANNEL_TYPE_VOLCENGINE,
  CHANNEL_TYPE_DOUBAO_VIDEO,
])
const UPSTREAM_TASK_ID_TYPES: ReadonlySet<number> = new Set([
  CHANNEL_TYPE_VOLCENGINE,
  CHANNEL_TYPE_DOUBAO_VIDEO,
  CHANNEL_TYPE_SEEDANCE_3RD,
])
const BASE_URL_FIELD_TYPES: ReadonlySet<number> = new Set([
  CHANNEL_TYPE_VOLCENGINE,
  CHANNEL_TYPE_VOLC_PASSTHROUGH,
])

const ASSET_LIBRARY_KEYS = [
  'byteplus_asset_enabled',
  'byteplus_access_key',
  'byteplus_secret_key',
  'byteplus_asset_group_id',
  'byteplus_project_name',
  'byteplus_region',
  'byteplus_moderation_skip',
  'asset_provider',
] as const
const VOLC_PASSTHROUGH_KEYS = [
  'volc_sign_region',
  'volc_openapi_endpoint',
  'volc_asset_quota_limit',
] as const

// 与经典版 EditChannelModal.jsx 的默认模型保持一致；65 与后端 seedance3rd.ModelList 一致。
const VIDEO_CHANNEL_DEFAULT_MODELS: Partial<Record<number, readonly string[]>> =
  {
    [CHANNEL_TYPE_AIGC_VIDEO]: [
      'Vidu-q2',
      'Vidu-q2-pro',
      'Vidu-q2-turbo',
      'Vidu-q3-pro',
      'Vidu-q3-turbo',
      'Vidu-template',
      'Kling-1.6',
      'Kling-2.0',
      'Kling-2.1',
      'Kling-2.5',
      'Kling-2.6',
      'Kling-3.0',
      'Kling-3.0-Omni',
      'Kling-O1',
      'Hunyuan-1.5',
      'Mingmou-1.0',
      'OS-2.0',
      'GV-3.1',
      'GV-3.1-fast',
      'Hailuo-02',
      'Hailuo-2.3',
      'Hailuo-2.3-fast',
      'SV-1.5-pro',
      'SV-1.0-pro',
      'SV-1.0-pro-fast',
      'SV-1.0-lite',
      'JV-3.0-pro',
    ],
    [CHANNEL_TYPE_SEEDANCE_3RD]: [
      'dreamina-seedance-2-0-260128',
      'dreamina-seedance-2-0-fast-260128',
      'dreamina-seedance-2-0-mini-260615',
      'dreamina-seedance-2-5-260628',
    ],
  }

export const videoChannelSettingsShape = {
  // 火山方舟 / 豆包视频的素材库预上传
  byteplus_asset_enabled: z.boolean().optional(),
  byteplus_access_key: z.string().optional(),
  byteplus_secret_key: z.string().optional(),
  byteplus_asset_group_id: z.string().optional(),
  byteplus_project_name: z.string().optional(),
  byteplus_region: z.string().optional(),
  byteplus_moderation_skip: z.boolean().optional(),
  asset_provider: z.enum(['byteplus', 'cloudwise']).optional(),
  // 字节火山透传的控制面签名；AK/SK/项目名复用上面三个 byteplus_* 键
  volc_sign_region: z.string().optional(),
  volc_openapi_endpoint: z.string().optional(),
  // 素材库条数上限，只给管理页对账用；0 表示未知或不限
  volc_asset_quota_limit: z.number().optional(),
  // Seedance 第三方的素材预上传
  seedance3rd_asset_enabled: z.boolean().optional(),
  // 火山方舟 / 豆包视频 / Seedance 第三方的上游任务 ID 透传
  expose_upstream_task_id: z.boolean().optional(),
}

const videoChannelSettingsSchema = z.object(videoChannelSettingsShape)

export type VideoChannelSettingValues = Required<
  z.infer<typeof videoChannelSettingsSchema>
>

export type VideoChannelFormSlice = Partial<VideoChannelSettingValues> & {
  type: number
  base_url?: string
}

export const VIDEO_CHANNEL_SETTINGS_DEFAULTS: VideoChannelSettingValues = {
  byteplus_asset_enabled: false,
  byteplus_access_key: '',
  byteplus_secret_key: '',
  byteplus_asset_group_id: '',
  byteplus_project_name: 'default',
  byteplus_region: 'cn-beijing',
  byteplus_moderation_skip: true,
  asset_provider: 'byteplus',
  seedance3rd_asset_enabled: false,
  expose_upstream_task_id: false,
  // 留空时后端按渠道 BaseURL 推导，不预填具体 region
  volc_sign_region: '',
  volc_openapi_endpoint: '',
  volc_asset_quota_limit: 0,
}

function textOr(value: unknown, fallback: string): string {
  return typeof value === 'string' && value !== '' ? value : fallback
}

/** 从已解析的渠道 settings 读出表单值；缺失或非法的值回落到默认值。 */
export function readVideoChannelSettings(
  settings: Record<string, unknown> | null | undefined
): VideoChannelSettingValues {
  const source = settings ?? {}
  const defaults = VIDEO_CHANNEL_SETTINGS_DEFAULTS
  return {
    byteplus_asset_enabled: source.byteplus_asset_enabled === true,
    byteplus_access_key: textOr(
      source.byteplus_access_key,
      defaults.byteplus_access_key
    ),
    byteplus_secret_key: textOr(
      source.byteplus_secret_key,
      defaults.byteplus_secret_key
    ),
    byteplus_asset_group_id: textOr(
      source.byteplus_asset_group_id,
      defaults.byteplus_asset_group_id
    ),
    byteplus_project_name: textOr(
      source.byteplus_project_name,
      defaults.byteplus_project_name
    ),
    byteplus_region: textOr(source.byteplus_region, defaults.byteplus_region),
    byteplus_moderation_skip: source.byteplus_moderation_skip !== false,
    // 与后端 ResolveAssetProvider 一致：未知值按官方处理
    asset_provider:
      source.asset_provider === 'cloudwise' ? 'cloudwise' : 'byteplus',
    seedance3rd_asset_enabled: source.seedance3rd_asset_enabled === true,
    expose_upstream_task_id: source.expose_upstream_task_id === true,
    volc_sign_region: textOr(
      source.volc_sign_region,
      defaults.volc_sign_region
    ),
    volc_openapi_endpoint: textOr(
      source.volc_openapi_endpoint,
      defaults.volc_openapi_endpoint
    ),
    volc_asset_quota_limit: Number(source.volc_asset_quota_limit) || 0,
  }
}

/** 把表单值写进 settings（原地修改）；渠道类型不对应的键会被删除。 */
export function writeVideoChannelSettings(
  settings: Record<string, unknown>,
  formData: VideoChannelFormSlice
): void {
  const type = formData.type

  if (type === CHANNEL_TYPE_VOLC_PASSTHROUGH) {
    // 66 的 AK/SK/项目名在下面的火山透传分支写入；素材库相关的旧键保留不删。
  } else if (ASSET_LIBRARY_TYPES.has(type)) {
    settings.byteplus_asset_enabled = formData.byteplus_asset_enabled === true
    settings.byteplus_access_key = (formData.byteplus_access_key || '').trim()
    settings.byteplus_secret_key = (formData.byteplus_secret_key || '').trim()
    settings.byteplus_asset_group_id = (
      formData.byteplus_asset_group_id || ''
    ).trim()
    settings.byteplus_project_name =
      (formData.byteplus_project_name || '').trim() || 'default'
    settings.byteplus_region =
      (formData.byteplus_region || '').trim() || 'cn-beijing'
    settings.byteplus_moderation_skip =
      formData.byteplus_moderation_skip !== false
    settings.asset_provider = formData.asset_provider || 'byteplus'
  } else {
    for (const key of ASSET_LIBRARY_KEYS) delete settings[key]
  }

  if (type === CHANNEL_TYPE_VOLC_PASSTHROUGH) {
    // 素材库上限官方查不到，只能手填；0、空、负数一律存 0
    settings.volc_asset_quota_limit = Math.max(
      0,
      Math.floor(Number(formData.volc_asset_quota_limit) || 0)
    )
    settings.volc_sign_region = (formData.volc_sign_region || '').trim()
    settings.volc_openapi_endpoint = (
      formData.volc_openapi_endpoint || ''
    ).trim()
    settings.byteplus_access_key = (formData.byteplus_access_key || '').trim()
    settings.byteplus_secret_key = (formData.byteplus_secret_key || '').trim()
    settings.byteplus_project_name =
      (formData.byteplus_project_name || '').trim() || 'default'
  } else {
    for (const key of VOLC_PASSTHROUGH_KEYS) delete settings[key]
  }

  if (type === CHANNEL_TYPE_SEEDANCE_3RD) {
    settings.seedance3rd_asset_enabled =
      formData.seedance3rd_asset_enabled === true
  } else {
    delete settings.seedance3rd_asset_enabled
  }

  if (UPSTREAM_TASK_ID_TYPES.has(type)) {
    settings.expose_upstream_task_id = formData.expose_upstream_task_id === true
  } else {
    delete settings.expose_upstream_task_id
  }
}

/** 素材库开启时：官方来源要 AK/SK，Cloudwise 复用渠道地址所以要求地址。 */
export function validateVideoChannelSettings(
  data: VideoChannelFormSlice,
  ctx: z.RefinementCtx
): void {
  if (!ASSET_LIBRARY_TYPES.has(data.type) || !data.byteplus_asset_enabled) {
    return
  }
  if (data.asset_provider === 'cloudwise') {
    if (!data.base_url?.trim()) {
      ctx.addIssue({
        code: 'custom',
        path: ['base_url'],
        message:
          'Third-party asset library reuses the channel API URL, please fill it in first',
      })
    }
    return
  }
  if (!data.byteplus_access_key?.trim()) {
    ctx.addIssue({
      code: 'custom',
      path: ['byteplus_access_key'],
      message: 'Official asset library requires an Access Key',
    })
  }
  if (!data.byteplus_secret_key?.trim()) {
    ctx.addIssue({
      code: 'custom',
      path: ['byteplus_secret_key'],
      message: 'Official asset library requires a Secret Key',
    })
  }
}

export function hasAssetLibrary(type: number): boolean {
  return ASSET_LIBRARY_TYPES.has(type)
}

export function hasSeedanceAssetUpload(type: number): boolean {
  return type === CHANNEL_TYPE_SEEDANCE_3RD
}

export function hasUpstreamTaskIdOption(type: number): boolean {
  return UPSTREAM_TASK_ID_TYPES.has(type)
}

export function hasVolcPassthroughSigning(type: number): boolean {
  return type === CHANNEL_TYPE_VOLC_PASSTHROUGH
}

/** 这些类型的地址输入由 fork 组件渲染，抽屉里上游的地址块要跳过。 */
export function ownsVideoChannelBaseUrl(type: number): boolean {
  return BASE_URL_FIELD_TYPES.has(type)
}

/** 新建渠道时，地址为空就填这个默认值。 */
export function getVideoChannelDefaultBaseUrl(
  type: number
): string | undefined {
  return type === CHANNEL_TYPE_VOLC_PASSTHROUGH ? ARK_CN_BASE_URL : undefined
}

/** "填充相关模型"用的固定模型列表；不是 fork 类型时返回 undefined。 */
export function getVideoChannelDefaultModels(
  type: number
): string[] | undefined {
  const models = VIDEO_CHANNEL_DEFAULT_MODELS[type]
  return models ? [...models] : undefined
}

/** fork 类型的密钥格式提示（翻译键）；不是 fork 类型时返回 undefined。 */
export function getVideoChannelKeyPrompt(type: number): string | undefined {
  return type === CHANNEL_TYPE_AIGC_VIDEO
    ? 'Format: SubAppId|SecretId|SecretKey[|Region], or an API key such as sk-xxx'
    : undefined
}
