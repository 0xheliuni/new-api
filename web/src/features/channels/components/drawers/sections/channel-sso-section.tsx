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
import { KeySquare, Loader2 } from 'lucide-react'
import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { PasswordInput } from '@/components/password-input'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { handleServerError } from '@/lib/handle-server-error'
import { createServerError } from '@/lib/server-error-message'

import {
  getChannelSSOConfig,
  getChannelSSOStatus,
  saveChannelSSOConfig,
} from '../../../sso/api'

const DEFAULT_ENTRY_PATH = '/dashboard'
const NOT_ENABLED_MESSAGE_KEY = 'Channel SSO is not enabled on this server'

/** Derives the default upstream console URL from the channel's own base URL. */
function originFromBaseUrl(value: string | undefined): string {
  const trimmed = value?.trim()
  if (!trimmed) return ''
  try {
    return new URL(trimmed).origin
  } catch {
    return ''
  }
}

type SSOFields = {
  baseUrl: string
  username: string
  entryPath: string
}

export type ChannelSSOSectionHandle = {
  /**
   * Called after the channel itself has just been saved. Saves the section's
   * fields only if they were actually changed; a failure here only shows an
   * error toast — it never rolls back the already-saved channel.
   */
  saveIfChanged: (channelId: number) => Promise<void>
  /**
   * Called instead of `saveIfChanged` when a channel was just created but the
   * server did not return the new channel's id, so this section's own
   * changes (if any) have nowhere to be saved to. Only toasts when there was
   * actually something unsaved.
   */
  notifyUnsavable: () => void
}

type ChannelSSOSectionProps = {
  /** null while creating a channel that has not been saved yet. */
  channelId: number | null
  isEditing: boolean
  /** The drawer is mounted while closed too; gate fetching on this. */
  open: boolean
  /** The channel's own base_url form value, used to default the console URL. */
  defaultBaseUrl?: string
  disabled?: boolean
}

export const ChannelSSOSection = forwardRef<
  ChannelSSOSectionHandle,
  ChannelSSOSectionProps
>(function ChannelSSOSection(props, ref) {
  const { t } = useTranslation()
  const [baseUrl, setBaseUrl] = useState('')
  const [username, setUsername] = useState('')
  const [password, setPassword] = useState('')
  const [entryPath, setEntryPath] = useState(DEFAULT_ENTRY_PATH)
  const [hasPassword, setHasPassword] = useState(false)
  const [configured, setConfigured] = useState(false)
  const [notEnabledMessage, setNotEnabledMessage] = useState<string | null>(
    null
  )
  const [isLoading, setIsLoading] = useState(false)
  const [isSaving, setIsSaving] = useState(false)
  const [isClearing, setIsClearing] = useState(false)
  const [clearConfirmOpen, setClearConfirmOpen] = useState(false)
  const initialRef = useRef<SSOFields>({
    baseUrl: '',
    username: '',
    entryPath: DEFAULT_ENTRY_PATH,
  })
  const loadedForRef = useRef<number | 'create' | null>(null)

  useEffect(() => {
    if (!props.open) {
      loadedForRef.current = null
      return
    }
    if (props.isEditing && props.channelId) {
      if (loadedForRef.current === props.channelId) return
      loadedForRef.current = props.channelId
      const channelId = props.channelId
      setIsLoading(true)
      setNotEnabledMessage(null)
      let cancelled = false
      void getChannelSSOConfig(channelId)
        .then((res) => {
          if (cancelled) return
          if (!res.success) {
            if (res.message_key === NOT_ENABLED_MESSAGE_KEY) {
              setNotEnabledMessage(t(NOT_ENABLED_MESSAGE_KEY))
              return
            }
            throw createServerError(res, t('Failed to load SSO settings'))
          }
          const data = res.data
          const next: SSOFields = {
            baseUrl: data?.base_url ?? '',
            username: data?.username ?? '',
            entryPath: data?.entry_path || DEFAULT_ENTRY_PATH,
          }
          setBaseUrl(next.baseUrl)
          setUsername(next.username)
          setEntryPath(next.entryPath)
          setHasPassword(Boolean(data?.has_password))
          setConfigured(Boolean(data?.configured))
          setPassword('')
          initialRef.current = next
        })
        .catch((error) => {
          if (cancelled) return
          handleServerError(error, t('Failed to load SSO settings'))
        })
        .finally(() => {
          if (!cancelled) setIsLoading(false)
        })
      return () => {
        cancelled = true
        // Let the next run (React StrictMode's re-mount, or reopening)
        // load again instead of skipping a load that was just cancelled.
        loadedForRef.current = null
      }
    }
    if (!props.isEditing && loadedForRef.current !== 'create') {
      loadedForRef.current = 'create'
      const next: SSOFields = {
        baseUrl: originFromBaseUrl(props.defaultBaseUrl),
        username: '',
        entryPath: DEFAULT_ENTRY_PATH,
      }
      setBaseUrl(next.baseUrl)
      setUsername('')
      setEntryPath(next.entryPath)
      setPassword('')
      setHasPassword(false)
      setConfigured(false)
      setNotEnabledMessage(null)
      initialRef.current = next
      // Edit mode learns this from the config request; when creating there
      // is no channel yet, so ask the server directly. Best-effort: on any
      // failure the fields stay editable and the save reports the error.
      let cancelled = false
      void getChannelSSOStatus()
        .then((res) => {
          if (cancelled || !res.success || res.data?.enabled !== false) return
          setNotEnabledMessage(t(NOT_ENABLED_MESSAGE_KEY))
        })
        .catch(() => {})
      return () => {
        cancelled = true
        // Let the next run (React StrictMode's re-mount, or reopening)
        // load again instead of skipping a load that was just cancelled.
        loadedForRef.current = null
      }
    }
    return undefined
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [props.open, props.isEditing, props.channelId])

  const isDirty =
    baseUrl !== initialRef.current.baseUrl ||
    username !== initialRef.current.username ||
    entryPath !== initialRef.current.entryPath ||
    password !== ''

  useImperativeHandle(
    ref,
    () => ({
      async saveIfChanged(channelId: number) {
        if (notEnabledMessage) return
        if (!isDirty) return
        setIsSaving(true)
        try {
          const res = await saveChannelSSOConfig(channelId, {
            base_url: baseUrl,
            username,
            password,
            entry_path: entryPath,
          })
          if (!res.success) {
            throw createServerError(res, t('Failed to save SSO settings'))
          }
          initialRef.current = { baseUrl, username, entryPath }
          setHasPassword(hasPassword || password !== '')
          setConfigured(true)
          setPassword('')
        } catch (error) {
          handleServerError(error, t('Failed to save SSO settings'))
        } finally {
          setIsSaving(false)
        }
      },
      notifyUnsavable() {
        if (!notEnabledMessage && isDirty) {
          toast.error(
            t(
              'Single sign-on settings were not saved; edit the channel to set them'
            )
          )
        }
      },
    }),
    [
      baseUrl,
      username,
      password,
      entryPath,
      hasPassword,
      isDirty,
      notEnabledMessage,
      t,
    ]
  )

  const handleClear = useCallback(async () => {
    if (!props.channelId) return
    const channelId = props.channelId
    setIsClearing(true)
    try {
      const res = await saveChannelSSOConfig(channelId, {
        base_url: '',
        username: '',
        password: '',
        entry_path: '',
        clear: true,
      })
      if (!res.success) {
        throw createServerError(res, t('Failed to clear SSO settings'))
      }
      const next: SSOFields = {
        baseUrl: originFromBaseUrl(props.defaultBaseUrl),
        username: '',
        entryPath: DEFAULT_ENTRY_PATH,
      }
      setBaseUrl(next.baseUrl)
      setUsername('')
      setEntryPath(next.entryPath)
      setPassword('')
      setHasPassword(false)
      setConfigured(false)
      initialRef.current = next
      toast.success(t('SSO configuration cleared'))
    } catch (error) {
      handleServerError(error, t('Failed to clear SSO settings'))
    } finally {
      setIsClearing(false)
      setClearConfirmOpen(false)
    }
  }, [props.channelId, props.defaultBaseUrl, t])

  // When the server reports the feature disabled, the fields and the clear
  // button stay visible but inert (readOnly inputs, disabled button) instead
  // of disappearing — there is nothing useful to edit, but the admin should
  // still see the section's shape and the reason it cannot be used.
  const fieldsReadOnly = Boolean(notEnabledMessage)
  const fieldsDisabled = Boolean(props.disabled || isLoading)
  const showClearButton = props.isEditing && (configured || fieldsReadOnly)

  return (
    <div className='border-border/60 flex flex-col gap-3 border-t pt-4'>
      <div className='flex items-center gap-2'>
        <KeySquare
          className='text-muted-foreground h-3.5 w-3.5'
          aria-hidden='true'
        />
        <h4 className='text-muted-foreground text-xs font-medium tracking-wide uppercase'>
          {t('Single Sign-On')}
        </h4>
        {isLoading && (
          <Loader2 className='text-muted-foreground size-3.5 animate-spin' />
        )}
      </div>

      {notEnabledMessage && (
        <Alert>
          <AlertDescription>{notEnabledMessage}</AlertDescription>
        </Alert>
      )}

      <div className='flex flex-col gap-4'>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Lets a super admin sign in to this channel’s upstream New API console without re-entering upstream credentials.'
          )}
        </p>

        <div className='grid gap-4 sm:grid-cols-2'>
          <div className='space-y-1.5'>
            <Label htmlFor='channel-sso-base-url'>
              {t('Upstream Console URL')}
            </Label>
            <Input
              id='channel-sso-base-url'
              value={baseUrl}
              readOnly={fieldsReadOnly}
              disabled={fieldsDisabled}
              onChange={(e) => setBaseUrl(e.target.value)}
              placeholder='https://console.example.com'
            />
          </div>

          <div className='space-y-1.5'>
            <Label htmlFor='channel-sso-username'>{t('Username')}</Label>
            <Input
              id='channel-sso-username'
              value={username}
              readOnly={fieldsReadOnly}
              disabled={fieldsDisabled}
              onChange={(e) => setUsername(e.target.value)}
              autoComplete='off'
            />
          </div>

          <div className='space-y-1.5'>
            <Label htmlFor='channel-sso-password'>{t('Password')}</Label>
            <PasswordInput
              id='channel-sso-password'
              value={password}
              readOnly={fieldsReadOnly}
              disabled={fieldsDisabled}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete='new-password'
              placeholder={
                hasPassword ? t('Saved — leave blank to keep') : undefined
              }
            />
          </div>

          <div className='space-y-1.5'>
            <Label htmlFor='channel-sso-entry-path'>{t('Entry Path')}</Label>
            <Input
              id='channel-sso-entry-path'
              value={entryPath}
              readOnly={fieldsReadOnly}
              disabled={fieldsDisabled}
              onChange={(e) => setEntryPath(e.target.value)}
              placeholder={DEFAULT_ENTRY_PATH}
            />
          </div>
        </div>

        {showClearButton && (
          <div>
            <Button
              type='button'
              variant='outline'
              size='sm'
              disabled={fieldsDisabled || fieldsReadOnly || isClearing}
              onClick={() => setClearConfirmOpen(true)}
            >
              {isClearing && <Loader2 className='mr-2 h-4 w-4 animate-spin' />}
              {t('Clear SSO configuration')}
            </Button>
          </div>
        )}

        {isSaving && (
          <p className='text-muted-foreground text-xs'>
            {t('Saving SSO settings…')}
          </p>
        )}
      </div>

      <ConfirmDialog
        open={clearConfirmOpen}
        onOpenChange={setClearConfirmOpen}
        title={t('Clear SSO configuration?')}
        desc={t(
          'This removes the saved upstream credentials for this channel. This action cannot be undone.'
        )}
        confirmText={t('Clear')}
        destructive
        isLoading={isClearing}
        handleConfirm={() => {
          void handleClear()
        }}
      />
    </div>
  )
})
