import { request } from './client'
import type { CreateUserPayload, UpdateUserPayload, User } from './types'

export function createUser(payload: CreateUserPayload): Promise<User> {
  return request<User>('/api/users/', { method: 'POST', body: payload })
}

export function patchUser(id: number, payload: UpdateUserPayload): Promise<User> {
  return request<User>(`/api/users/${id}/`, { method: 'PATCH', body: payload })
}

export function uploadUserAvatar(id: number, file: Blob): Promise<User> {
  return request<User>(`/api/users/${id}/avatar/`, { method: 'PUT', body: file })
}

export function deleteUser(id: number): Promise<void> {
  return request<void>(`/api/users/${id}/`, { method: 'DELETE' })
}
