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
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { getRoleLabel, ROLE, type RoleValue } from '@/lib/roles'
import { getServerErrorMessage } from '@/lib/server-error-message'

import {
  checkChannelSSO,
  getChannelSSOConfig,
  openChannelSSO,
  validateChannelSSO,
  type ChannelSSOApiResponse,
} from './api'

export type ChannelSSOStepKey = 'verify' | 'login' | 'open'
export type ChannelSSOStepStatus = 'pending' | 'running' | 'success' | 'error'

export type ChannelSSOStep = {
  key: ChannelSSOStepKey
  status: ChannelSSOStepStatus
  detail?: string
}

export type ChannelSSOHeader = {
  /** The upstream's own site name (from `/api/status`), once step 1 succeeds. */
  siteName: string | null
  /** The configured upstream console URL, fetched alongside step 1 for display only. */
  baseUrl: string | null
}

const INITIAL_STEPS: ChannelSSOStep[] = [
  { key: 'verify', status: 'pending' },
  { key: 'login', status: 'pending' },
  { key: 'open', status: 'pending' },
]

const NOT_CONFIGURED_MESSAGE_KEY = 'Channel SSO is not configured'
const ENTER_DELAY_MS = 500

/** role >= 100 is Super Admin, >= 10 is Admin, otherwise User. */
function resolveRoleBucket(role: number): RoleValue {
  if (role >= ROLE.SUPER_ADMIN) return ROLE.SUPER_ADMIN
  if (role >= ROLE.ADMIN) return ROLE.ADMIN
  return ROLE.USER
}

export function useChannelSSOFlow(channelId: number) {
  const { t } = useTranslation()
  const [steps, setSteps] = useState<ChannelSSOStep[]>(INITIAL_STEPS)
  const [header, setHeader] = useState<ChannelSSOHeader>({
    siteName: null,
    baseUrl: null,
  })
  const [isNotConfigured, setIsNotConfigured] = useState(false)

  // Guards the mount effect against React StrictMode's double-invoke so the
  // flow starts exactly once per run; retry() bypasses
  // this guard by calling runFlow directly.
  const hasStartedRef = useRef(false)
  const isMountedRef = useRef(true)
  const runIdRef = useRef(0)
  const enterTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const activeControllerRef = useRef<AbortController | null>(null)

  const updateStep = useCallback(
    (key: ChannelSSOStepKey, patch: Partial<Omit<ChannelSSOStep, 'key'>>) => {
      setSteps((previous) =>
        previous.map((step) =>
          step.key === key ? { ...step, ...patch } : step
        )
      )
    },
    []
  )

  const runFlow = useCallback(
    async (runId: number) => {
      const isCurrent = () => isMountedRef.current && runIdRef.current === runId
      const controller = new AbortController()
      activeControllerRef.current = controller

      setSteps(INITIAL_STEPS)
      setHeader({ siteName: null, baseUrl: null })
      setIsNotConfigured(false)

      // Display-only lookup of the configured upstream URL; independent of
      // the steps below and never fatal to the flow.
      void (async () => {
        try {
          const res = await getChannelSSOConfig(channelId, controller.signal)
          if (!isCurrent() || !res.success) return
          const baseUrl = res.data?.base_url
          if (!baseUrl) return
          setHeader((previous) => ({ ...previous, baseUrl }))
        } catch {
          /* decorative only */
        }
      })()

      updateStep('verify', { status: 'running' })

      let checkRes: ChannelSSOApiResponse<{
        session_id: string
        site_name: string
        username: string
        role: number
        reused: boolean
      }>
      try {
        checkRes = await checkChannelSSO(channelId, controller.signal)
      } catch (error) {
        if (!isCurrent()) return
        updateStep('verify', {
          status: 'error',
          detail: getServerErrorMessage(error, t('Something went wrong!')),
        })
        return
      }
      if (!isCurrent()) return
      if (!checkRes.success || !checkRes.data) {
        setIsNotConfigured(checkRes.message_key === NOT_CONFIGURED_MESSAGE_KEY)
        updateStep('verify', {
          status: 'error',
          detail: getServerErrorMessage(checkRes, t('Something went wrong!')),
        })
        return
      }

      const checkData = checkRes.data
      const roleLabel = getRoleLabel(resolveRoleBucket(checkData.role))
      updateStep('verify', {
        status: 'success',
        detail: `${roleLabel} · ${checkData.username}`,
      })
      setHeader((previous) => ({ ...previous, siteName: checkData.site_name }))

      updateStep('login', { status: 'running' })
      let validateRes: ChannelSSOApiResponse<{ username: string; role: number }>
      try {
        validateRes = await validateChannelSSO(
          checkData.session_id,
          controller.signal
        )
      } catch (error) {
        if (!isCurrent()) return
        updateStep('login', {
          status: 'error',
          detail: getServerErrorMessage(error, t('Something went wrong!')),
        })
        return
      }
      if (!isCurrent()) return
      if (!validateRes.success) {
        updateStep('login', {
          status: 'error',
          detail: getServerErrorMessage(
            validateRes,
            t('Something went wrong!')
          ),
        })
        return
      }
      updateStep('login', {
        status: 'success',
        detail: checkData.reused
          ? t('Reused existing session')
          : t('Signed in again'),
      })

      updateStep('open', { status: 'running' })
      let openRes: ChannelSSOApiResponse<{ enter_url: string }>
      try {
        openRes = await openChannelSSO(checkData.session_id, controller.signal)
      } catch (error) {
        if (!isCurrent()) return
        updateStep('open', {
          status: 'error',
          detail: getServerErrorMessage(error, t('Something went wrong!')),
        })
        return
      }
      if (!isCurrent()) return
      if (!openRes.success || !openRes.data) {
        updateStep('open', {
          status: 'error',
          detail: getServerErrorMessage(openRes, t('Something went wrong!')),
        })
        return
      }
      updateStep('open', { status: 'success', detail: t('Entering…') })

      const enterUrl = openRes.data.enter_url
      enterTimerRef.current = setTimeout(() => {
        if (runIdRef.current === runId) window.location.replace(enterUrl)
      }, ENTER_DELAY_MS)
    },
    [channelId, t, updateStep]
  )

  const retry = useCallback(() => {
    if (enterTimerRef.current) {
      clearTimeout(enterTimerRef.current)
      enterTimerRef.current = null
    }
    activeControllerRef.current?.abort()
    runIdRef.current += 1
    void runFlow(runIdRef.current)
  }, [runFlow])

  useEffect(() => {
    isMountedRef.current = true
    if (!hasStartedRef.current) {
      hasStartedRef.current = true
      void runFlow(runIdRef.current)
    }
    // Only flip the mounted flag here; aborting the in-flight request on
    // cleanup would also cancel the one real run when React StrictMode
    // double-invokes this effect in development (mount → cleanup → mount).
    // The isCurrent() checks inside runFlow already stop state updates once
    // truly unmounted; the request itself is short-lived and self-resolves.
    return () => {
      isMountedRef.current = false
    }
    // Intentionally runs only once per mount (guarded above); retry() is the
    // only other entry point into runFlow.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(
    () => () => {
      if (enterTimerRef.current) clearTimeout(enterTimerRef.current)
    },
    []
  )

  return { steps, retry, header, isNotConfigured }
}
