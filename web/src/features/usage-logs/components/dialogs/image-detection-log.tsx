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
import axios from 'axios'
import type { TFunction } from 'i18next'
import {
  AlertTriangle,
  Clock,
  Copy,
  Download,
  ExternalLink,
  ImageUp,
  Loader2,
  X,
} from 'lucide-react'
import { useId, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import { getImageDetectionImage } from '../../api'
import type { LogOtherData } from '../../types'

type DetectionSteps = NonNullable<LogOtherData['detection_steps']>

type DetectionLogEntry = {
  key: 'resolution' | 'metadata' | 'content'
  title: string
  message: string
  detail?: string
  // "waiting" is a check that has not run yet, not a failure.
  tone: 'fail' | 'error' | 'waiting'
}

const LOG_TONE_STYLES = {
  fail: {
    item: 'border-rose-500/25 bg-rose-500/5 dark:border-rose-400/25',
    badge: 'bg-rose-500 text-white dark:bg-rose-400 dark:text-rose-950',
  },
  error: {
    item: 'border-orange-500/25 bg-orange-500/5 dark:border-orange-400/25',
    badge: 'bg-orange-500/15 text-orange-600 dark:text-orange-400',
  },
  waiting: {
    item: 'border-sky-500/25 bg-sky-500/5 dark:border-sky-400/25',
    badge: 'bg-sky-500/15 text-sky-600 dark:text-sky-400',
  },
} satisfies Record<DetectionLogEntry['tone'], { item: string; badge: string }>

const OPENAI_VERIFY_URL = 'https://openai.com/verify/'

const IMAGE_EXTENSIONS: Record<string, string> = {
  'image/png': '.png',
  'image/jpeg': '.jpg',
  'image/webp': '.webp',
  'image/gif': '.gif',
}

/**
 * Builds the detection log: one entry per check that did not pass, with the
 * reason recorded by the backend, plus an official check that is waiting out
 * an OpenAI rate limit. Passed, skipped and unchecked steps are left out so
 * the log only lists what needs attention.
 */
function detectionLogEntries(
  steps: DetectionSteps,
  t: TFunction
): DetectionLogEntry[] {
  const metadataErrors: Record<string, string> = {
    tool_unavailable: t(
      'c2patool is not installed or the configured path is invalid'
    ),
    timeout: t('c2patool timed out'),
  }
  // Why a manifest that was found did not count as OpenAI credentials.
  const metadataReasons: Record<string, string> = {
    invalid_credentials: t(
      'The image was changed after its C2PA credentials were signed'
    ),
    other_issuer: t('The C2PA credentials were not issued by OpenAI'),
    no_ai_action: t('The C2PA credentials do not record AI generation'),
    untrusted_credentials: t(
      'The C2PA signature does not chain to a trusted certificate'
    ),
  }
  const contentErrors: Record<string, string> = {
    no_api_key: t(
      'No OpenAI API key is configured, so the official check did not run'
    ),
    invalid_api_key: t(
      'The OpenAI API key was rejected; check the key in the image detection settings'
    ),
    no_access: t(
      'This OpenAI organization has no access to the Content Provenance API yet; request access first'
    ),
    rejected: t('OpenAI rejected the image'),
    too_large: t('The image is over the 50 MiB limit of the official check'),
    unsupported_image: t(
      'The image format is not supported by the official check'
    ),
    request_failed: t('Could not reach the OpenAI API'),
    timeout: t('The OpenAI API did not respond in time'),
    server_error: t('The OpenAI API kept failing with server errors'),
    http_error: t('The OpenAI API rejected the request'),
    image_lost: t('The image kept for the check was lost'),
    inconclusive: t('The official check returned no usable result'),
  }

  const entries: DetectionLogEntry[] = []
  if (steps.resolution === 'fail') {
    entries.push({
      key: 'resolution',
      title: t('Resolution'),
      tone: 'fail',
      message:
        steps.request_size && steps.actual_size
          ? t('Requested {{requested}}, got {{actual}}', {
              requested: steps.request_size,
              actual: steps.actual_size,
            })
          : t('The image size does not match the request'),
    })
  }
  if (steps.metadata === 'fail') {
    entries.push({
      key: 'metadata',
      title: t('Metadata'),
      tone: 'fail',
      message:
        metadataReasons[steps.metadata_error ?? ''] ??
        t('No C2PA credentials showing OpenAI generation were found'),
    })
  } else if (steps.metadata === 'error') {
    entries.push({
      key: 'metadata',
      title: t('Metadata'),
      tone: 'error',
      message:
        metadataErrors[steps.metadata_error ?? ''] ??
        t('c2patool could not read the image'),
    })
  }
  if (steps.content === 'fail') {
    entries.push({
      key: 'content',
      title: t('Content provenance'),
      tone: 'fail',
      message: t(
        'The official check found neither OpenAI C2PA credentials nor a SynthID watermark'
      ),
    })
  } else if (steps.content === 'error') {
    entries.push({
      key: 'content',
      title: t('Content provenance'),
      tone: 'error',
      message:
        contentErrors[steps.content_error ?? ''] ??
        t('The official check could not be completed'),
      detail: steps.content_error_detail || undefined,
    })
  } else if (
    steps.content === 'pending' &&
    steps.content_error === 'rate_limited'
  ) {
    entries.push({
      key: 'content',
      title: t('Content provenance'),
      tone: 'waiting',
      message: t(
        'OpenAI rate limit reached; a later scheduled run checks the image'
      ),
      detail: steps.content_error_detail || undefined,
    })
  }
  return entries
}

/** Lists the checks that did not pass; renders nothing when all of them did. */
export function DetectionLog(props: { steps: DetectionSteps }) {
  const { t } = useTranslation()
  const headingId = useId()
  const entries = detectionLogEntries(props.steps, t)
  if (entries.length === 0) return null

  return (
    <div className='border-border/60 space-y-1.5 border-t pt-2.5'>
      <p
        id={headingId}
        className='text-muted-foreground text-[11px] font-semibold'
      >
        {t('Detection log')}
      </p>
      <ul aria-labelledby={headingId} className='space-y-1.5'>
        {entries.map((entry) => (
          <li
            key={entry.key}
            className={cn(
              'flex min-w-0 items-start gap-2 rounded-md border px-2.5 py-2',
              LOG_TONE_STYLES[entry.tone].item
            )}
          >
            <span
              className={cn(
                'mt-px flex size-4 shrink-0 items-center justify-center rounded-full',
                LOG_TONE_STYLES[entry.tone].badge
              )}
            >
              {entry.tone === 'fail' && (
                <X className='size-2.5' strokeWidth={3.5} aria-hidden='true' />
              )}
              {entry.tone === 'error' && (
                <AlertTriangle className='size-2.5' aria-hidden='true' />
              )}
              {entry.tone === 'waiting' && (
                <Clock className='size-2.5' aria-hidden='true' />
              )}
            </span>
            <div className='min-w-0 flex-1 space-y-0.5'>
              <p className='text-xs leading-snug font-medium'>{entry.title}</p>
              <p className='text-muted-foreground text-xs leading-snug'>
                {entry.message}
              </p>
              {entry.detail && (
                <p className='text-muted-foreground/80 font-mono text-[10px] leading-snug break-all'>
                  {entry.detail}
                </p>
              )}
            </div>
          </li>
        ))}
      </ul>
    </div>
  )
}

/**
 * Shows the official Content Provenance answer per signal. C2PA and SynthID
 * are read independently: re-encoding, screenshots and crops strip C2PA while
 * SynthID survives, so either can be found without the other. Renders nothing
 * until the API has answered.
 */
export function ProvenanceSignals(props: { steps: DetectionSteps }) {
  const { t } = useTranslation()
  const headingId = useId()
  const outcomes: Record<string, { label: string; variant: StatusVariant }> = {
    detected: { label: t('Detected'), variant: 'success' },
    not_detected: { label: t('Not detected'), variant: 'neutral' },
  }
  const validationStates: Record<string, string> = {
    trusted: t('Trusted'),
    valid: t('Valid'),
    invalid: t('Invalid'),
    not_present: t('Not present'),
  }
  const c2paState = props.steps.provenance_c2pa_state ?? ''
  const signals = [
    {
      name: 'C2PA',
      outcome: props.steps.provenance_c2pa ?? '',
      state: validationStates[c2paState] ?? c2paState,
      issuer: props.steps.provenance_issuer ?? '',
    },
    {
      name: 'SynthID',
      outcome: props.steps.provenance_synthid ?? '',
      state: '',
      issuer: '',
    },
  ].filter((signal) => signal.outcome !== '')
  if (signals.length === 0) return null

  return (
    <div className='border-border/60 space-y-1.5 border-t pt-2.5'>
      <p
        id={headingId}
        className='text-muted-foreground text-[11px] font-semibold'
      >
        {t('Provenance signals')}
      </p>
      <ul aria-labelledby={headingId} className='space-y-1'>
        {signals.map((signal) => (
          <li
            key={signal.name}
            className='flex min-w-0 items-center gap-2 text-xs'
          >
            <span className='w-14 shrink-0 font-medium'>{signal.name}</span>
            <StatusBadge
              label={outcomes[signal.outcome]?.label ?? signal.outcome}
              variant={outcomes[signal.outcome]?.variant ?? 'neutral'}
              size='sm'
              copyable={false}
            />
            {signal.state && (
              <span className='text-muted-foreground shrink-0'>
                {signal.state}
              </span>
            )}
            {signal.issuer && (
              <span
                className='text-muted-foreground min-w-0 truncate'
                title={signal.issuer}
              >
                {signal.issuer}
              </span>
            )}
          </li>
        ))}
      </ul>
      {props.steps.provenance_model && (
        <p className='text-muted-foreground text-[11px]'>
          {t('Model')}:{' '}
          <span className='text-foreground font-mono'>
            {props.steps.provenance_model}
          </span>
        </p>
      )}
    </div>
  )
}

// Clipboards only accept PNG images, so other formats are re-encoded first.
async function toPngBlob(image: Blob): Promise<Blob> {
  if (image.type === 'image/png') return image
  const bitmap = await createImageBitmap(image)
  const canvas = document.createElement('canvas')
  canvas.width = bitmap.width
  canvas.height = bitmap.height
  canvas.getContext('2d')?.drawImage(bitmap, 0, 0)
  bitmap.close()
  return new Promise((resolve, reject) => {
    canvas.toBlob(
      (png) => (png ? resolve(png) : reject(new Error('PNG encoding failed'))),
      'image/png'
    )
  })
}

/**
 * Lets root download or copy the image kept for a content provenance check
 * that could not finish, and opens the official OpenAI verify page to check
 * it by hand.
 */
export function ManualVerification(props: { requestId: string }) {
  const { t } = useTranslation()
  const headingId = useId()
  const [busy, setBusy] = useState<'download' | 'copy' | null>(null)

  const notifyFailure = (error: unknown) => {
    if (axios.isAxiosError(error) && error.response?.status === 404) {
      toast.error(t('The image is no longer available'))
      return
    }
    toast.error(t('Failed to load the image'))
  }

  const handleDownload = async () => {
    setBusy('download')
    try {
      const image = await getImageDetectionImage(props.requestId)
      const url = URL.createObjectURL(image)
      const link = document.createElement('a')
      link.href = url
      link.download = `image-detection-${props.requestId}${IMAGE_EXTENSIONS[image.type] ?? ''}`
      document.body.append(link)
      link.click()
      link.remove()
      URL.revokeObjectURL(url)
    } catch (error) {
      notifyFailure(error)
    } finally {
      setBusy(null)
    }
  }

  const handleCopy = async () => {
    if (typeof ClipboardItem === 'undefined' || !navigator.clipboard?.write) {
      toast.error(
        t('This browser cannot copy images. Download the original instead.')
      )
      return
    }
    setBusy('copy')
    // The write has to start inside the click, so the clipboard item gets a
    // promise that resolves once the image has been loaded.
    const png = getImageDetectionImage(props.requestId).then(toPngBlob)
    try {
      await Promise.all([
        navigator.clipboard.write([new ClipboardItem({ 'image/png': png })]),
        png,
      ])
      toast.success(t('Image copied'))
    } catch (error) {
      notifyFailure(error)
    } finally {
      setBusy(null)
    }
  }

  return (
    <div
      role='group'
      aria-labelledby={headingId}
      className='space-y-2 rounded-md border border-dashed border-sky-500/35 bg-sky-500/5 p-2.5 dark:border-sky-400/30'
    >
      <div className='flex items-start gap-2'>
        <ImageUp
          className='mt-0.5 size-3.5 shrink-0 text-sky-600 dark:text-sky-400'
          aria-hidden='true'
        />
        <div className='min-w-0 space-y-0.5'>
          <p id={headingId} className='text-xs font-semibold'>
            {t('Manual verification')}
          </p>
          <p className='text-muted-foreground text-[11px] leading-snug'>
            {t(
              'Download the original image and upload it on the OpenAI verify page. A copied image loses its C2PA metadata, so only the SynthID watermark can be checked.'
            )}
          </p>
        </div>
      </div>
      <div className='flex flex-wrap gap-1.5'>
        <Button
          type='button'
          size='sm'
          onClick={handleDownload}
          disabled={busy !== null}
        >
          {busy === 'download' ? (
            <Loader2 className='animate-spin' aria-hidden='true' />
          ) : (
            <Download aria-hidden='true' />
          )}
          {t('Download original')}
        </Button>
        <Button
          type='button'
          size='sm'
          variant='outline'
          onClick={handleCopy}
          disabled={busy !== null}
        >
          {busy === 'copy' ? (
            <Loader2 className='animate-spin' aria-hidden='true' />
          ) : (
            <Copy aria-hidden='true' />
          )}
          {t('Copy image')}
        </Button>
        <Button
          role='link'
          size='sm'
          variant='ghost'
          render={
            <a
              href={OPENAI_VERIFY_URL}
              target='_blank'
              rel='noopener noreferrer'
            />
          }
        >
          <ExternalLink aria-hidden='true' />
          {t('Open the OpenAI verify page')}
        </Button>
      </div>
    </div>
  )
}
