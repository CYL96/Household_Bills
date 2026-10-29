import { reactive } from 'vue'
import {
  getMe,
  login as loginRequest,
  logout as logoutRequest,
} from '../api/auth'
import { getErrorMessage, setUnauthorizedHandler } from '../api/client'
import type {
  Household,
  LoginPayload,
  MeResponse,
  User,
} from '../api/types'

interface AuthState {
  user: User | null
  household: Household | null
  members: User[]
  bootError: string | null
}

export const authState = reactive<AuthState>({
  user: null,
  household: null,
  members: [],
  bootError: null,
})

function applyMe(me: MeResponse): void {
  authState.user = me.user
  authState.household = me.user ? (me.household ?? null) : null
  authState.members = me.user ? (me.members ?? []) : []
}

function clearAuth(): void {
  authState.user = null
  authState.household = null
  authState.members = []
}

setUnauthorizedHandler(clearAuth)

export async function bootstrapAuth(): Promise<void> {
  authState.bootError = null
  try {
    const me = await getMe()
    applyMe(me)
  } catch (error) {
    clearAuth()
    authState.bootError = getErrorMessage(error)
  }
}

export async function loginUser(payload: LoginPayload): Promise<void> {
  const me = await loginRequest(payload)
  applyMe(me)
  authState.bootError = null
}

export function addAuthMember(member: User): void {
  if (authState.members.some((existing) => existing.id === member.id)) return
  authState.members = [...authState.members, member]
}

export async function refreshAuth(): Promise<void> {
  const me = await getMe()
  applyMe(me)
}

export async function logoutUser(): Promise<void> {
  let requestError: unknown = null
  try {
    await logoutRequest()
  } catch (error) {
    requestError = error
  } finally {
    clearAuth()
  }
  if (requestError) throw requestError
}
