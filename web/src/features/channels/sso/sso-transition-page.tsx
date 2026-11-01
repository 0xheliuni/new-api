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
import { Check, CircleDashed, Loader2, Pencil, XCircle } from 'lucide-react'
import { useEffect, useRef } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  Card,
  CardContent,
  CardDescription,
  CardFooter,
  CardHeader,
  CardTitle,
} from '@/components/ui/card'
import { cn } from '@/lib/utils'

import {
  useChannelSSOFlow,
  type ChannelSSOStep,
  type ChannelSSOStepKey,
} from './use-channel-sso-flow'

const STEP_ORDER: ChannelSSOStepKey[] = ['verify', 'login', 'open']

function StepIcon(props: { status: ChannelSSOStep['status'] }) {
  if (props.status === 'running') {
    return (
      <Loader2
        className='text-primary size-5 shrink-0 animate-spin'
        aria-hidden='true'
      />
    )
  }
  if (props.status === 'success') {
    return <Check className='text-success size-5 shrink-0' aria-hidden='true' />
  }
  if (props.status === 'error') {
    return (
      <XCircle
        className='text-destructive size-5 shrink-0'
        aria-hidden='true'
      />
    )
  }
  return (
    <CircleDashed
      className='text-muted-foreground size-5 shrink-0'
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
  return (
    <li className='flex items-start gap-3'>
      <span role='img' aria-label={props.statusLabel} className='pt-0.5'>
        <StepIcon status={props.step.status} />
      </span>
      <div className='min-w-0 flex-1'>
        <p
          className={cn(
            'text-sm font-medium',
            props.step.status === 'pending' && 'text-muted-foreground'
          )}
        >
          {props.label}
        </p>
        <p
          className={cn(
            'text-xs',
            props.step.status === 'error'
              ? 'text-destructive'
              : 'text-muted-foreground'
          )}
        >
          {props.step.detail ?? props.description}
        </p>
      </div>
    </li>
  )
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

  useEffect(() => {
    if (failedStep) actionsRef.current?.focus()
    // Only re-run when which step failed changes, not on every render while
    // it stays failed — otherwise focus would be yanked back repeatedly.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [failedStep?.key])

  return (
    <div className='flex h-full w-full items-center justify-center p-4 sm:p-6'>
      <Card className='w-full max-w-md'>
        <CardHeader>
          <CardTitle>{t('Single sign-on')}</CardTitle>
          <CardDescription className='truncate'>
            {header.siteName ?? t('Connecting…')}
            {header.baseUrl && (
              <span className='block truncate'>{header.baseUrl}</span>
            )}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <ol aria-live='polite' className='flex flex-col gap-4'>
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
        </CardContent>
        {failedStep && (
          <CardFooter>
            <div
              ref={actionsRef}
              tabIndex={-1}
              className='flex w-full flex-wrap gap-2 outline-none'
            >
              <Button type='button' onClick={retry}>
                {t('Retry')}
              </Button>
              <Button
                type='button'
                variant='outline'
                render={<Link to='/channels' />}
              >
                {t('Back to channels')}
              </Button>
              {isNotConfigured && (
                <Button
                  type='button'
                  variant='outline'
                  render={<Link to='/channels' />}
                >
                  <Pencil className='size-4' aria-hidden='true' />
                  {t('Edit channel')}
                </Button>
              )}
            </div>
          </CardFooter>
        )}
      </Card>
    </div>
  )
}
