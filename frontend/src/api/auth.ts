import { request } from './client'
import type { LoginPayload, MeResponse } from './types'

export function getMe(): Promise<MeResponse> {
  return request<MeResponse>('/api/auth/me/')
}

export function login(payload: LoginPayload): Promise<MeResponse> {
  return request<MeResponse>('/api/auth/login/', { method: 'POST', body: payload })
}

export function logout(): Promise<void> {
  return request<void>('/api/auth/logout/', { method: 'POST' })
}
