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
import { zodResolver } from '@hookform/resolvers/zod'
import { useEffect, useMemo, useRef, useState } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import * as z from 'zod'

import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Separator } from '@/components/ui/separator'
import { Switch } from '@/components/ui/switch'
import { api } from '@/lib/api'

import {
  SettingsControlChildren,
  SettingsControlGroup,
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'
import { safeNumberFieldProps } from '../utils/numeric-field'

// ─────────────────────────────────────────────────────────────────────────────
// InstallC2PAToolButton — one-click download + install inside the container
// ─────────────────────────────────────────────────────────────────────────────

type InstallState = 'idle' | 'installing' | 'success' | 'error'

function InstallC2PAToolButton(props: { onInstalled: (path: string) => void }) {
  const { t } = useTranslation()
  const [state, setState] = useState<InstallState>('idle')
  const [errorMsg, setErrorMsg] = useState('')

  const handleInstall = async () => {
    setState('installing')
    setErrorMsg('')
    try {
      const res = await api.post<{
        success: boolean
        message: string
        data?: { path: string }
      }>('/api/image-detection/install-c2pa')
      if (res.data.success && res.data.data?.path) {
        setState('success')
        props.onInstalled(res.data.data.path)
        toast.success(
          t('c2patool installed to {{path}}', { path: res.data.data.path })
        )
      } else {
        setState('error')
        setErrorMsg(res.data.message || t('Installation failed'))
        toast.error(res.data.message || t('Installation failed'))
      }
    } catch (err: unknown) {
      const msg = err instanceof Error ? err.message : t('Installation failed')
      setState('error')
      setErrorMsg(msg)
      toast.error(msg)
    }
  }

  return (
    <div className='flex min-w-0 flex-col gap-2 sm:flex-row sm:items-center sm:gap-3'>
      <Button
        type='button'
        variant='outline'
        size='sm'
        className='shrink-0'
        disabled={state === 'installing'}
        onClick={handleInstall}
      >
        {state === 'installing'
          ? t('Installing…')
          : t('Download & install c2patool')}
      </Button>
      <div className='min-w-0 space-y-0.5'>
        {state === 'success' && (
          <p className='text-xs text-green-600 dark:text-green-400'>
            {t('Installed successfully. Path has been saved.')}
          </p>
        )}
        {state === 'error' && (
          <p className='text-destructive text-xs'>{errorMsg}</p>
        )}
        <p className='text-muted-foreground text-xs'>
          {t(
            'Downloads v0.28.2 from GitHub for the current platform (linux/amd64, windows/amd64, or macOS) and writes it to /usr/local/bin inside the container.'
          )}
        </p>
      </div>
    </div>
  )
}

const imageDetectionSchema = z.object({
  global_enabled: z.boolean(),
  global_sample_rate: z.coerce.number().min(0).max(1),
  scheduled_enabled: z.boolean(),
  scheduled_cron: z.string().min(1),
  scheduled_batch_limit: z.coerce.number().min(1).max(1000),
  channel_scope: z.enum(['all', 'selected']),
  selected_channel_ids: z.string(),
  check_resolution: z.boolean(),
  check_c2pa: z.boolean(),
  check_synth_id: z.boolean(),
  synth_id_force: z.boolean(),
  provenance_api_key: z.string(),
  synth_id_rate_per_minute: z.coerce.number().min(1),
  synth_id_rate_per_day: z.coerce.number().min(1),
  c2pa_tool_path: z.string(),
  c2pa_timeout_seconds: z.coerce.number().min(1).max(30),
})

type ImageDetectionFormInput = z.input<typeof imageDetectionSchema>
type ImageDetectionFormValues = z.output<typeof imageDetectionSchema>

type FlatImageDetectionDefaults = {
  'image_detection_setting.global_enabled': boolean
  'image_detection_setting.global_sample_rate': number
  'image_detection_setting.scheduled_enabled': boolean
  'image_detection_setting.scheduled_cron': string
  'image_detection_setting.scheduled_batch_limit': number
  'image_detection_setting.channel_scope': string
  'image_detection_setting.selected_channel_ids': number[]
  'image_detection_setting.check_resolution': boolean
  'image_detection_setting.check_c2pa': boolean
  'image_detection_setting.check_synth_id': boolean
  'image_detection_setting.synth_id_force': boolean
  'image_detection_setting.provenance_api_key': string
  'image_detection_setting.synth_id_rate_per_minute': number
  'image_detection_setting.synth_id_rate_per_day': number
  'image_detection_setting.c2pa_tool_path': string
  'image_detection_setting.c2pa_timeout_seconds': number
}

type ImageDetectionSectionProps = {
  defaultValues: FlatImageDetectionDefaults
}

const buildFormDefaults = (
  defaults: ImageDetectionSectionProps['defaultValues']
): ImageDetectionFormInput => ({
  global_enabled: defaults['image_detection_setting.global_enabled'],
  global_sample_rate: defaults['image_detection_setting.global_sample_rate'],
  scheduled_enabled: defaults['image_detection_setting.scheduled_enabled'],
  scheduled_cron: defaults['image_detection_setting.scheduled_cron'],
  scheduled_batch_limit:
    defaults['image_detection_setting.scheduled_batch_limit'],
  channel_scope: defaults['image_detection_setting.channel_scope'] as
    | 'all'
    | 'selected',
  selected_channel_ids: (
    defaults['image_detection_setting.selected_channel_ids'] ?? []
  ).join(', '),
  check_resolution: defaults['image_detection_setting.check_resolution'],
  check_c2pa: defaults['image_detection_setting.check_c2pa'],
  check_synth_id: defaults['image_detection_setting.check_synth_id'],
  synth_id_force: defaults['image_detection_setting.synth_id_force'] ?? false,
  // The options API never returns a saved key, so the field always starts empty.
  provenance_api_key: '',
  synth_id_rate_per_minute:
    defaults['image_detection_setting.synth_id_rate_per_minute'],
  synth_id_rate_per_day:
    defaults['image_detection_setting.synth_id_rate_per_day'],
  c2pa_tool_path: defaults['image_detection_setting.c2pa_tool_path'] ?? '',
  c2pa_timeout_seconds:
    defaults['image_detection_setting.c2pa_timeout_seconds'],
})

const normalizeDefaults = (
  defaults: ImageDetectionSectionProps['defaultValues']
): FlatImageDetectionDefaults => ({
  'image_detection_setting.global_enabled':
    defaults['image_detection_setting.global_enabled'],
  'image_detection_setting.global_sample_rate':
    defaults['image_detection_setting.global_sample_rate'],
  'image_detection_setting.scheduled_enabled':
    defaults['image_detection_setting.scheduled_enabled'],
  'image_detection_setting.scheduled_cron':
    defaults['image_detection_setting.scheduled_cron'],
  'image_detection_setting.scheduled_batch_limit':
    defaults['image_detection_setting.scheduled_batch_limit'],
  'image_detection_setting.channel_scope':
    defaults['image_detection_setting.channel_scope'],
  'image_detection_setting.selected_channel_ids':
    defaults['image_detection_setting.selected_channel_ids'] ?? [],
  'image_detection_setting.check_resolution':
    defaults['image_detection_setting.check_resolution'],
  'image_detection_setting.check_c2pa':
    defaults['image_detection_setting.check_c2pa'],
  'image_detection_setting.check_synth_id':
    defaults['image_detection_setting.check_synth_id'],
  'image_detection_setting.synth_id_force':
    defaults['image_detection_setting.synth_id_force'] ?? false,
  'image_detection_setting.provenance_api_key': '',
  'image_detection_setting.synth_id_rate_per_minute':
    defaults['image_detection_setting.synth_id_rate_per_minute'],
  'image_detection_setting.synth_id_rate_per_day':
    defaults['image_detection_setting.synth_id_rate_per_day'],
  'image_detection_setting.c2pa_tool_path':
    defaults['image_detection_setting.c2pa_tool_path'] ?? '',
  'image_detection_setting.c2pa_timeout_seconds':
    defaults['image_detection_setting.c2pa_timeout_seconds'],
})

const parseChannelIds = (raw: string): number[] =>
  raw
    .split(/[\s,]+/)
    .map((part) => part.trim())
    .filter(Boolean)
    .map((part) => Number(part))
    .filter((n) => Number.isFinite(n) && n > 0)

const normalizeFormValues = (
  values: ImageDetectionFormValues
): FlatImageDetectionDefaults => ({
  'image_detection_setting.global_enabled': values.global_enabled,
  'image_detection_setting.global_sample_rate': values.global_sample_rate,
  'image_detection_setting.scheduled_enabled': values.scheduled_enabled,
  'image_detection_setting.scheduled_cron': values.scheduled_cron,
  'image_detection_setting.scheduled_batch_limit': values.scheduled_batch_limit,
  'image_detection_setting.channel_scope': values.channel_scope,
  'image_detection_setting.selected_channel_ids': parseChannelIds(
    values.selected_channel_ids
  ),
  'image_detection_setting.check_resolution': values.check_resolution,
  'image_detection_setting.check_c2pa': values.check_c2pa,
  'image_detection_setting.check_synth_id': values.check_synth_id,
  'image_detection_setting.synth_id_force': values.synth_id_force,
  'image_detection_setting.provenance_api_key':
    values.provenance_api_key.trim(),
  'image_detection_setting.synth_id_rate_per_minute':
    values.synth_id_rate_per_minute,
  'image_detection_setting.synth_id_rate_per_day': values.synth_id_rate_per_day,
  'image_detection_setting.c2pa_tool_path': values.c2pa_tool_path,
  'image_detection_setting.c2pa_timeout_seconds': values.c2pa_timeout_seconds,
})

export function ImageDetectionSection({
  defaultValues,
}: ImageDetectionSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()
  const baselineRef = useRef<FlatImageDetectionDefaults>(
    normalizeDefaults(defaultValues)
  )
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(normalizeDefaults(defaultValues))
  )

  const formDefaults = useMemo(
    () => buildFormDefaults(defaultValues),
    [defaultValues]
  )

  const form = useForm<
    ImageDetectionFormInput,
    unknown,
    ImageDetectionFormValues
  >({
    resolver: zodResolver(imageDetectionSchema),
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  useEffect(() => {
    const normalized = normalizeDefaults(defaultValues)
    const serialized = JSON.stringify(normalized)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = normalized
    baselineSerializedRef.current = serialized
  }, [defaultValues])

  const globalEnabled = form.watch('global_enabled')
  const scheduledEnabled = form.watch('scheduled_enabled')
  const channelScope = form.watch('channel_scope')
  const checkC2PA = form.watch('check_c2pa')
  const checkSynthID = form.watch('check_synth_id')

  const onSubmit = async (values: ImageDetectionFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = (
      Object.keys(normalized) as Array<keyof FlatImageDetectionDefaults>
    ).filter((key) => {
      const a = normalized[key]
      const b = baselineRef.current[key]
      // A blank key field keeps the saved key; only a newly entered one is sent.
      if (key === 'image_detection_setting.provenance_api_key') return a !== ''
      if (Array.isArray(a) && Array.isArray(b)) {
        return JSON.stringify(a) !== JSON.stringify(b)
      }
      return a !== b
    })

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const key of updates) {
      const raw = normalized[key]
      const value = Array.isArray(raw) ? JSON.stringify(raw) : raw
      await updateOption.mutateAsync({
        key,
        value: value as string | number | boolean,
      })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
  }

  return (
    <SettingsSection title={t('Image Detection')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          {/* ── Global detection ──────────────────────────────── */}
          <div className='flex min-w-0 flex-col gap-3'>
            <h4 className='text-sm font-medium'>{t('Global detection')}</h4>
            <SettingsControlGroup>
              <FormField
                control={form.control}
                name='global_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Global detection')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Run L1/L2/L3 pipeline on every sampled image request'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <SettingsControlChildren>
                <div className='grid grid-cols-2 items-start gap-x-5 gap-y-0'>
                  <FormField
                    control={form.control}
                    name='global_sample_rate'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Sample rate')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={0}
                            max={1}
                            step={0.01}
                            {...safeNumberFieldProps(field)}
                            disabled={!globalEnabled}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            '0.0 to 1.0 — fraction of requests that trigger detection'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              </SettingsControlChildren>
            </SettingsControlGroup>
          </div>

          <Separator />

          {/* ── Scheduled sampling ────────────────────────────── */}
          <div className='flex min-w-0 flex-col gap-3'>
            <h4 className='text-sm font-medium'>{t('Scheduled sampling')}</h4>
            <SettingsControlGroup>
              <FormField
                control={form.control}
                name='scheduled_enabled'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Scheduled sampling')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Process pending content provenance checks on a cron schedule'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <SettingsControlChildren>
                <div className='grid grid-cols-2 items-start gap-x-5 gap-y-0'>
                  <FormField
                    control={form.control}
                    name='scheduled_cron'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Cron expression')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder='0 */2 * * *'
                            value={field.value}
                            onChange={(e) => field.onChange(e.target.value)}
                            disabled={!scheduledEnabled}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('Standard 5-field cron, e.g. 0 */2 * * *')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='scheduled_batch_limit'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('Batch limit')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={1000}
                            step={1}
                            {...safeNumberFieldProps(field)}
                            disabled={!scheduledEnabled}
                          />
                        </FormControl>
                        <FormDescription>
                          {t('Max pending records to process per run')}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              </SettingsControlChildren>
            </SettingsControlGroup>
          </div>

          <Separator />

          {/* ── Channel scope ─────────────────────────────────── */}
          <div className='flex min-w-0 flex-col gap-3'>
            <h4 className='text-sm font-medium'>{t('Channel Scope')}</h4>
            <div className='grid grid-cols-2 items-start gap-x-5 gap-y-0'>
              <FormField
                control={form.control}
                name='channel_scope'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Channel Scope')}</FormLabel>
                    <Select
                      items={[
                        { value: 'all', label: t('All channels') },
                        { value: 'selected', label: t('Selected channels') },
                      ]}
                      value={field.value}
                      onValueChange={field.onChange}
                    >
                      <FormControl>
                        <SelectTrigger className='w-full'>
                          <SelectValue />
                        </SelectTrigger>
                      </FormControl>
                      <SelectContent alignItemWithTrigger={false}>
                        <SelectGroup>
                          <SelectItem value='all'>
                            {t('All channels')}
                          </SelectItem>
                          <SelectItem value='selected'>
                            {t('Selected channels')}
                          </SelectItem>
                        </SelectGroup>
                      </SelectContent>
                    </Select>
                    <FormDescription>
                      {t('Which channels participate in detection')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />
              {channelScope === 'selected' && (
                <FormField
                  control={form.control}
                  name='selected_channel_ids'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Channel IDs (comma-separated)')}
                      </FormLabel>
                      <FormControl>
                        <Input
                          placeholder='1, 2, 3'
                          value={field.value}
                          onChange={(e) => field.onChange(e.target.value)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Enter channel IDs separated by commas')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              )}
            </div>
          </div>

          <Separator />

          {/* ── Detection checks ──────────────────────────────── */}
          <div className='flex min-w-0 flex-col gap-3'>
            <h4 className='text-sm font-medium'>{t('Detection Checks')}</h4>

            {/* Resolution */}
            <SettingsControlGroup>
              <FormField
                control={form.control}
                name='check_resolution'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Resolution check')}</FormLabel>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
            </SettingsControlGroup>

            {/* C2PA */}
            <SettingsControlGroup>
              <FormField
                control={form.control}
                name='check_c2pa'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('C2PA metadata check')}</FormLabel>
                      <FormDescription>
                        {t(
                          'Only a trusted OpenAI signature that records AI generation counts as a pass; every other image goes on to the content provenance check'
                        )}
                      </FormDescription>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <SettingsControlChildren>
                <div className='grid grid-cols-2 items-start gap-x-5 gap-y-0'>
                  <FormField
                    control={form.control}
                    name='c2pa_tool_path'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('c2patool path')}</FormLabel>
                        <FormControl>
                          <Input
                            placeholder={t(
                              'Leave empty to use c2patool from PATH'
                            )}
                            value={field.value}
                            onChange={(e) => field.onChange(e.target.value)}
                            disabled={!checkC2PA}
                          />
                        </FormControl>
                        <FormDescription>
                          {t(
                            'Absolute path to the c2patool binary. Use the button below to download and install it inside the container.'
                          )}
                        </FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='c2pa_timeout_seconds'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>{t('c2patool timeout (seconds)')}</FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            max={30}
                            step={1}
                            {...safeNumberFieldProps(field)}
                            disabled={!checkC2PA}
                          />
                        </FormControl>
                        <FormDescription>&nbsp;</FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <div className='col-span-2 mt-1'>
                    <InstallC2PAToolButton
                      onInstalled={(path) =>
                        form.setValue('c2pa_tool_path', path, {
                          shouldDirty: true,
                          shouldTouch: true,
                        })
                      }
                    />
                  </div>
                </div>
              </SettingsControlChildren>
            </SettingsControlGroup>

            {/* Content provenance (OpenAI API: C2PA + SynthID) */}
            <SettingsControlGroup>
              <FormField
                control={form.control}
                name='check_synth_id'
                render={({ field }) => (
                  <SettingsSwitchItem>
                    <SettingsSwitchContent>
                      <FormLabel>{t('Content provenance check')}</FormLabel>
                    </SettingsSwitchContent>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </SettingsSwitchItem>
                )}
              />
              <SettingsControlChildren className='space-y-2'>
                <FormField
                  control={form.control}
                  name='synth_id_force'
                  render={({ field }) => (
                    <SettingsSwitchItem>
                      <SettingsSwitchContent>
                        <FormLabel>
                          {t('Force content provenance check')}
                        </FormLabel>
                        <FormDescription>
                          {t(
                            'Run the content provenance check even when the C2PA metadata already shows an OpenAI signal'
                          )}
                        </FormDescription>
                      </SettingsSwitchContent>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                          disabled={!checkSynthID}
                        />
                      </FormControl>
                    </SettingsSwitchItem>
                  )}
                />
                <FormField
                  control={form.control}
                  name='provenance_api_key'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('OpenAI API key')}</FormLabel>
                      <FormControl>
                        <Input
                          type='password'
                          autoComplete='new-password'
                          placeholder={t('Enter a new key to update')}
                          value={field.value}
                          onChange={(event) =>
                            field.onChange(event.target.value)
                          }
                          disabled={!checkSynthID}
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Used for content provenance checks. Leave blank to keep the saved key. Without a saved key, the OPENAI_API_KEY_DETECTION or OPENAI_API_KEY environment variable is used.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
                <div className='grid grid-cols-2 items-start gap-x-5 gap-y-0'>
                  <FormField
                    control={form.control}
                    name='synth_id_rate_per_minute'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>
                          {t('Content provenance max calls/minute')}
                        </FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            step={1}
                            {...safeNumberFieldProps(field)}
                            disabled={!checkSynthID}
                          />
                        </FormControl>
                        <FormDescription>&nbsp;</FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                  <FormField
                    control={form.control}
                    name='synth_id_rate_per_day'
                    render={({ field }) => (
                      <FormItem>
                        <FormLabel>
                          {t('Content provenance max calls/day')}
                        </FormLabel>
                        <FormControl>
                          <Input
                            type='number'
                            min={1}
                            step={1}
                            {...safeNumberFieldProps(field)}
                            disabled={!checkSynthID}
                          />
                        </FormControl>
                        <FormDescription>&nbsp;</FormDescription>
                        <FormMessage />
                      </FormItem>
                    )}
                  />
                </div>
              </SettingsControlChildren>
            </SettingsControlGroup>
          </div>
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
