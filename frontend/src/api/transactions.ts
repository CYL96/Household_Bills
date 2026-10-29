import { request } from './client'
import type { Transaction, TransactionFilters, TransactionPayload } from './types'

export function listTransactions(filters: TransactionFilters = {}): Promise<Transaction[]> {
  const params = new URLSearchParams()
  if (filters.month) params.set('month', filters.month)
  if (filters.account !== undefined) params.set('account', String(filters.account))
  if (filters.kind) params.set('kind', filters.kind)
  if (filters.q) params.set('q', filters.q)
  const query = params.toString()
  return request<Transaction[]>(`/api/transactions/${query ? `?${query}` : ''}`)
}

export function createTransaction(payload: TransactionPayload): Promise<Transaction> {
  return request<Transaction>('/api/transactions/', { method: 'POST', body: payload })
}

export function updateTransaction(id: number, payload: TransactionPayload): Promise<Transaction> {
  return request<Transaction>(`/api/transactions/${id}/`, { method: 'PATCH', body: payload })
}

export function deleteTransaction(id: number): Promise<void> {
  return request<void>(`/api/transactions/${id}/`, { method: 'DELETE' })
}
