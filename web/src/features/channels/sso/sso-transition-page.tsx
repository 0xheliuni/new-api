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
import { Link } from '@tanstack/react-router'
import { Check, Loader2, X } from 'lucide-react'
import { useEffect, useId, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

import {
  useChannelSSOFlow,
  type ChannelSSOStep,
  type ChannelSSOStepKey,
} from './use-channel-sso-flow'

const STEP_ORDER: ChannelSSOStepKey[] = ['verify', 'login', 'open']

type Outcome = 'running' | 'success' | 'error'

// This page is a standalone white screen, deliberately independent of the
// console theme, so the colors below are fixed instead of theme tokens.

function StepIcon(props: { status: ChannelSSOStep['status'] }) {
  if (props.status === 'running') {
    return (
      <Loader2
        className='size-4 animate-spin text-neutral-500'
        aria-hidden='true'
      />
    )
  }
  if (props.status === 'success') {
    return (
      <span className='flex size-5 items-center justify-center rounded-full bg-green-600'>
        <Check
          className='size-3 text-white'
          strokeWidth={3}
          aria-hidden='true'
        />
      </span>
    )
  }
  if (props.status === 'error') {
    return (
      <span className='flex size-5 items-center justify-center rounded-full bg-red-600'>
        <X className='size-3 text-white' strokeWidth={3} aria-hidden='true' />
      </span>
    )
  }
  return (
    <span
      className='size-5 rounded-full border-2 border-neutral-200'
      aria-hidden='true'
    />
  )
}

function StepRow(props: {
  step: ChannelSSOStep
  label: string
  description: string
  statusLabel: string
}) {
  const { status } = props.step
  return (
    <li className='flex items-start gap-3'>
      <span
        role='img'
        aria-label={props.statusLabel}
        className='flex size-5 shrink-0 items-center justify-center'
      >
        <StepIcon status={status} />
      </span>
      <div className='min-w-0 flex-1'>
        <p
          className={cn(
            'text-sm leading-5 font-medium',
            status === 'pending' ? 'text-neutral-400' : 'text-neutral-900'
          )}
        >
          {props.label}
        </p>
        <p
          className={cn(
            'mt-0.5 text-xs break-words',
            status === 'error' ? 'text-red-600' : 'text-neutral-500'
          )}
        >
          {props.step.detail ?? props.description}
        </p>
      </div>
    </li>
  )
}

function ResultMark(props: { outcome: Outcome }) {
  if (props.outcome === 'success') {
    return (
      <span className='animate-in fade-in zoom-in-50 flex size-20 items-center justify-center rounded-full bg-green-600 shadow-[0_8px_24px_rgba(22,163,74,0.35)] duration-300'>
        <Check
          className='size-11 text-white'
          strokeWidth={3}
          aria-hidden='true'
        />
      </span>
    )
  }
  if (props.outcome === 'error') {
    return (
      <span className='animate-in fade-in zoom-in-50 flex size-20 items-center justify-center rounded-full bg-red-600 shadow-[0_8px_24px_rgba(220,38,38,0.3)] duration-300'>
        <X className='size-11 text-white' strokeWidth={3} aria-hidden='true' />
      </span>
    )
  }
  return (
    <span className='flex size-20 items-center justify-center rounded-full bg-neutral-100'>
      <Loader2
        className='size-9 animate-spin text-neutral-400'
        aria-hidden='true'
      />
    </span>
  )
}

function hostOf(url: string | null): string | null {
  if (!url) return null
  try {
    return new URL(url).host
  } catch {
    return url
  }
}

type SSOTransitionPageProps = {
  channelId: number
}

export function SSOTransitionPage(props: SSOTransitionPageProps) {
  const { t } = useTranslation()
  const { steps, retry, header, isNotConfigured } = useChannelSSOFlow(
    props.channelId
  )
  const actionsRef = useRef<HTMLDivElement>(null)
  const resultLabelId = useId()

  const stepMeta: Record<
    ChannelSSOStepKey,
    { label: string; description: string }
  > = {
    verify: {
      label: t('Verify'),
      description: t(
        'Confirm the upstream site is reachable and read the upstream account role.'
      ),
    },
    login: {
      label: t('Sign in'),
      description: t('Confirm the upstream session is valid.'),
    },
    open: {
      label: t('Open'),
      description: t('Enter the upstream console.'),
    },
  }

  const statusLabels: Record<ChannelSSOStep['status'], string> = {
    pending: t('Pending'),
    running: t('In progress'),
    success: t('Success'),
    error: t('Error'),
  }

  const stepsByKey = new Map(steps.map((step) => [step.key, step]))
  const failedStep = steps.find((step) => step.status === 'error')
  let outcome: Outcome = 'running'
  if (failedStep) outcome = 'error'
  else if (steps.every((step) => step.status === 'success')) outcome = 'success'
  const resultLabels: Record<Outcome, string> = {
    running: t('Signing in to the upstream site…'),
    success: t('Signed in to the upstream site'),
    error: t('Sign-in failed'),
  }

  const siteTitle =
    header.siteName ?? hostOf(header.baseUrl) ?? t('Connecting…')

  useEffect(() => {
    if (failedStep) actionsRef.current?.focus()
    // Only re-run when which step failed changes, not on every render while
    // it stays failed; otherwise focus would be yanked back repeatedly.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [failedStep?.key])

  return (
    <main className='flex min-h-svh w-full items-center justify-center bg-white px-4 py-10 text-neutral-900'>
      <section className='w-full max-w-sm rounded-2xl border border-neutral-200 bg-white px-8 py-10 shadow-[0_1px_2px_rgba(0,0,0,0.04),0_12px_32px_rgba(0,0,0,0.06)]'>
        <header className='text-center'>
          <h1 className='truncate text-xl font-semibold tracking-tight'>
            {siteTitle}
          </h1>
          {header.baseUrl && (
            <p className='mt-1 truncate text-xs text-neutral-400'>
              {header.baseUrl}
            </p>
          )}
        </header>

        <ol aria-live='polite' className='mt-8 flex flex-col gap-5'>
          {STEP_ORDER.map((key) => {
            const step = stepsByKey.get(key)
            if (!step) return null
            return (
              <StepRow
                key={key}
                step={step}
                label={stepMeta[key].label}
                description={stepMeta[key].description}
                statusLabel={statusLabels[step.status]}
              />
            )
          })}
        </ol>

        <div className='mt-10 flex flex-col items-center gap-3'>
          <span role='img' aria-labelledby={resultLabelId}>
            <ResultMark outcome={outcome} />
          </span>
          <p
            id={resultLabelId}
            className={cn(
              'text-sm font-medium',
              outcome === 'success' && 'text-green-700',
              outcome === 'error' && 'text-red-600',
              outcome === 'running' && 'text-neutral-500'
            )}
          >
            {resultLabels[outcome]}
          </p>
        </div>

        {failedStep && (
          <div
            ref={actionsRef}
            tabIndex={-1}
            className='mt-6 flex flex-wrap justify-center gap-2 outline-none'
          >
            <Button type='button' size='sm' onClick={retry}>
              {t('Retry')}
            </Button>
            <Button
              type='button'
              size='sm'
              variant='outline'
              render={<Link to='/channels' />}
            >
              {isNotConfigured ? t('Edit channel') : t('Back to channels')}
            </Button>
          </div>
        )}
      </section>
    </main>
  )
}
