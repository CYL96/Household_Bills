import { request } from './client'
import type { Account, AccountPayload } from './types'

export function listAccounts(): Promise<Account[]> {
  return request<Account[]>('/api/accounts/')
}

export function createAccount(payload: AccountPayload): Promise<Account> {
  return request<Account>('/api/accounts/', { method: 'POST', body: payload })
}

export function updateAccount(id: number, payload: AccountPayload): Promise<Account> {
  return request<Account>(`/api/accounts/${id}/`, { method: 'PATCH', body: payload })
}
