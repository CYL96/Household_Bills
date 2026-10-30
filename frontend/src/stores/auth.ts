import { computed, reactive } from 'vue'
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

/** 成员名单与人数不包含系统管理员；账户归属选项同样只使用该名单。 */
export const familyMembers = computed(() =>
  authState.members.filter((member) => !member.is_admin),
)

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

export function clearAuthState(): void {
  clearAuth()
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

/** 同步更新当前登录用户与成员名单中的对应条目。 */
export function updateAuthUser(updated: User): void {
  if (authState.user?.id === updated.id) authState.user = updated
  authState.members = authState.members.map((member) =>
    member.id === updated.id ? updated : member,
  )
}

export function removeAuthMember(id: number): void {
  authState.members = authState.members.filter((member) => member.id !== id)
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
