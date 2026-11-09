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
import { useMutation } from '@tanstack/react-query'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Dialog } from '@/components/dialog'
import { JsonCodeEditor } from '@/components/json-code-editor'
import { Button } from '@/components/ui/button'
import { Combobox } from '@/components/ui/combobox'
import { Label } from '@/components/ui/label'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  previewTaskPluginConfig,
  type TaskPluginConfigPreviewRequest,
} from '../../api'

const OPENAI_VIDEO_ENTRY = 'openai_video'
const ALIBABA_NATIVE_ENTRY =
  '/ali/api/v1/services/aigc/video-generation/video-synthesis'

const SAMPLE_REQUESTS: Record<string, unknown> = {
  [OPENAI_VIDEO_ENTRY]: {
    model: 'wan3.0-video',
    prompt: 'A cat running on the grass',
    seconds: 5,
    resolution: '720P',
  },
  [ALIBABA_NATIVE_ENTRY]: {
    model: 'wan3.0-video',
    input: { prompt: 'A cat running on the grass' },
    parameters: { resolution: '720P', duration: 5 },
  },
}

const SAMPLE_SUBMIT_RESPONSE = {
  code: 200,
  message: 'success',
  data: { id: 'relay-task-1', status: 'PENDING' },
}

const SAMPLE_QUERY_RESPONSE = {
  code: 200,
  data: {
    task_id: 'relay-task-1',
    task_status: 'COMPLETED',
    result: { output: 'https://cdn.example/video.mp4' },
    usage: { duration: 5, resolution: '720P' },
  },
}

function formatJson(value: unknown): string {
  return JSON.stringify(value, null, 2)
}

type PreviewJsonFieldProps = {
  label: string
  value: string
  onChange?: (value: string) => void
}

function PreviewJsonField(props: PreviewJsonFieldProps) {
  return (
    <div className='grid gap-2'>
      <Label>{props.label}</Label>
      <JsonCodeEditor
        value={props.value}
        onChange={props.onChange ?? (() => undefined)}
        readOnly={!props.onChange}
        ariaLabel={props.label}
        heightClassName='h-40 min-h-40 max-h-40'
      />
    </div>
  )
}

type RelayConfigPreviewDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  config: string
  channelType: number
  taskPluginKey?: string
  baseUrl?: string
  model: string
}

export function RelayConfigPreviewDialog(props: RelayConfigPreviewDialogProps) {
  const { t } = useTranslation()
  const [entry, setEntry] = useState(OPENAI_VIDEO_ENTRY)
  const [requestText, setRequestText] = useState(() =>
    formatJson(SAMPLE_REQUESTS[OPENAI_VIDEO_ENTRY])
  )
  const [submitResponseText, setSubmitResponseText] = useState(() =>
    formatJson(SAMPLE_SUBMIT_RESPONSE)
  )
  const [queryResponseText, setQueryResponseText] = useState(() =>
    formatJson(SAMPLE_QUERY_RESPONSE)
  )
  const [inputError, setInputError] = useState('')
  const preview = useMutation({
    mutationFn: previewTaskPluginConfig,
    meta: { errorToast: false },
  })

  const entryOptions = [
    { value: OPENAI_VIDEO_ENTRY, label: t('OpenAI video (/v1/videos)') },
    {
      value: ALIBABA_NATIVE_ENTRY,
      label: t('Alibaba native (/ali/api/v1/...)'),
    },
  ]
  const errorLabels: Record<string, string> = {
    submit: t('Submit request'),
    submit_result: t('Task ID'),
    query_result: t('Query result'),
  }

  const handleEntryChange = (value: string | null) => {
    if (!value) return
    setEntry(value)
    setRequestText(formatJson(SAMPLE_REQUESTS[value]))
  }

  // A result describes the configuration it was run with, which can change
  // while the dialog is closed; the sample inputs and the entry are kept.
  const handleOpenChange = (open: boolean) => {
    if (!open) {
      preview.reset()
      setInputError('')
    }
    props.onOpenChange(open)
  }

  const parseField = (text: string, field: string): unknown => {
    if (!text.trim()) return undefined
    try {
      return JSON.parse(text)
    } catch {
      throw new Error(t('Invalid JSON: {{field}}', { field }))
    }
  }

  const handleRun = () => {
    setInputError('')
    let payload: TaskPluginConfigPreviewRequest
    try {
      payload = {
        type: props.channelType,
        task_plugin_key: props.taskPluginKey,
        base_url: props.baseUrl,
        config: parseField(props.config, t('Relay configuration')),
        model: props.model,
        entry,
        request: parseField(requestText, t('Sample client request')),
        submit_response: parseField(
          submitResponseText,
          t('Relay submit response')
        ),
        query_response: parseField(
          queryResponseText,
          t('Relay query response')
        ),
      }
    } catch (error: unknown) {
      preview.reset()
      setInputError(
        error instanceof Error ? error.message : t('Preview failed')
      )
      return
    }
    preview.mutate(payload)
  }

  const result = preview.data
  const runError =
    inputError ||
    (preview.error
      ? getServerErrorMessage(preview.error, t('Preview failed'))
      : '')
  const stepErrors = Object.entries(result?.errors ?? {}).filter(
    ([, message]) => message
  )

  return (
    <Dialog
      open={props.open}
      onOpenChange={handleOpenChange}
      title={t('Relay configuration preview')}
      description={t('No network request is sent and nothing is billed.')}
      contentClassName='sm:max-w-4xl'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button variant='outline' onClick={() => handleOpenChange(false)}>
            {t('Close')}
          </Button>
          <Button onClick={handleRun} disabled={preview.isPending}>
            {t('Run preview')}
          </Button>
        </>
      }
    >
      <div className='grid gap-2'>
        <Label htmlFor='relay-preview-entry'>{t('Client entry')}</Label>
        <Combobox
          id='relay-preview-entry'
          options={entryOptions}
          value={entry}
          onValueChange={handleEntryChange}
          className='w-full min-w-0'
        />
      </div>
      <PreviewJsonField
        label={t('Sample client request')}
        value={requestText}
        onChange={setRequestText}
      />
      <PreviewJsonField
        label={t('Relay submit response')}
        value={submitResponseText}
        onChange={setSubmitResponseText}
      />
      <PreviewJsonField
        label={t('Relay query response')}
        value={queryResponseText}
        onChange={setQueryResponseText}
      />
      {runError && (
        <p role='alert' className='text-destructive text-sm'>
          {runError}
        </p>
      )}
      {result && (
        <div className='space-y-4 border-t pt-4'>
          {stepErrors.map(([step, message]) => (
            <p key={step} role='alert' className='text-destructive text-sm'>
              {`${errorLabels[step] ?? step}: ${message}`}
            </p>
          ))}
          {result.submit && (
            <PreviewJsonField
              label={t('Submit request')}
              value={formatJson(result.submit)}
            />
          )}
          {result.reservation && (
            <PreviewJsonField
              label={t('Reserved usage')}
              value={formatJson(result.reservation)}
            />
          )}
          {result.submit_result && (
            <PreviewJsonField
              label={t('Task ID')}
              value={formatJson(result.submit_result)}
            />
          )}
          {result.query && (
            <PreviewJsonField
              label={t('Query request')}
              value={formatJson(result.query)}
            />
          )}
          {result.query_result && (
            <PreviewJsonField
              label={t('Query result')}
              value={formatJson(result.query_result)}
            />
          )}
          {result.settlement && (
            <PreviewJsonField
              label={t('Settled usage')}
              value={formatJson(result.settlement)}
            />
          )}
        </div>
      )}
    </Dialog>
  )
}
