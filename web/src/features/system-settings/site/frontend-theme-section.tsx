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
import { useEffect } from 'react'
import { useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import * as z from 'zod'

import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const frontendThemeSchema = z.object({
  'theme.frontend': z.enum(['default', 'classic']),
})

type FrontendThemeFormValues = z.infer<typeof frontendThemeSchema>

type FrontendThemeSectionProps = {
  defaultValue: string
}

export function FrontendThemeSection({ defaultValue }: FrontendThemeSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const form = useForm<FrontendThemeFormValues>({
    resolver: zodResolver(frontendThemeSchema),
    defaultValues: {
      'theme.frontend': (defaultValue === 'classic' ? 'classic' : 'default'),
    },
  })

  useEffect(() => {
    form.reset({
      'theme.frontend': (defaultValue === 'classic' ? 'classic' : 'default'),
    })
  }, [defaultValue, form])

  const onSubmit = async (values: FrontendThemeFormValues) => {
    await updateOption('theme.frontend', values['theme.frontend'])
  }

  return (
    <SettingsSection
      title={t('Frontend Theme')}
      description={t('Switch between the default and classic frontend interface. Takes effect on next page load.')}
    >
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <FormField
            control={form.control}
            name='theme.frontend'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Frontend Theme')}</FormLabel>
                <FormDescription>
                  {t('Default: the new frontend interface. Classic: the legacy frontend interface.')}
                </FormDescription>
                <FormControl>
                  <Select
                    value={field.value}
                    onValueChange={field.onChange}
                  >
                    <SelectTrigger className='w-48'>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value='default'>{t('Default')}</SelectItem>
                      <SelectItem value='classic'>{t('Classic')}</SelectItem>
                    </SelectContent>
                  </Select>
                </FormControl>
                <FormMessage />
              </FormItem>
            )}
          />
          <SettingsPageFormActions />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
