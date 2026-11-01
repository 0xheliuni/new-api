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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { SSOTransitionPage } from '@/features/channels/sso/sso-transition-page'
import { resolveAuthentication } from '@/lib/auth-session'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

// Lives outside the `_authenticated` layout so the transition page renders
// on its own (no console sidebar or header), with the same sign-in check.
export const Route = createFileRoute('/sso/channel/$channelId')({
  beforeLoad: async ({ location, params }) => {
    await resolveAuthentication()

    const { auth } = useAuthStore.getState()
    if (!auth.user || !auth.accessToken) {
      throw redirect({ to: '/sign-in', search: { redirect: location.href } })
    }
    if (auth.user.role < ROLE.SUPER_ADMIN) {
      throw redirect({ to: '/403' })
    }

    const channelId = Number(params.channelId)
    if (!Number.isInteger(channelId) || channelId <= 0) {
      throw redirect({ to: '/channels' })
    }
  },
  component: RouteComponent,
})

function RouteComponent() {
  const { channelId } = Route.useParams()
  return <SSOTransitionPage channelId={Number(channelId)} />
}
