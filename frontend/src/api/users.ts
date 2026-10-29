import { request } from './client'
import type { CreateUserPayload, User } from './types'

export function listUsers(): Promise<User[]> {
  return request<User[]>('/api/users/')
}

export function createUser(payload: CreateUserPayload): Promise<User> {
  return request<User>('/api/users/', { method: 'POST', body: payload })
}
