import { request } from './client'
import type {
  Transaction,
  TransactionFilters,
  TransactionPayload,
  TransactionTotals,
} from './types'

/** Serializes every supported list/totals filter identically for both endpoints. */
function filterParams(filters: TransactionFilters): URLSearchParams {
  const params = new URLSearchParams()
  if (filters.month) params.set('month', filters.month)
  if (filters.start) params.set('start', filters.start)
  if (filters.end) params.set('end', filters.end)
  if (filters.account !== undefined) params.set('account', String(filters.account))
  if (filters.kind) params.set('kind', filters.kind)
  if (filters.q) params.set('q', filters.q)
  for (const category of filters.category ?? []) params.append('category', category)
  return params
}

function withQuery(path: string, filters: TransactionFilters): string {
  const query = filterParams(filters).toString()
  return query ? `${path}?${query}` : path
}

export function listTransactions(filters: TransactionFilters = {}): Promise<Transaction[]> {
  return request<Transaction[]>(withQuery('/api/transactions/', filters))
}

export function getTransactionTotals(
  filters: TransactionFilters = {},
): Promise<TransactionTotals> {
  return request<TransactionTotals>(withQuery('/api/transactions/totals/', filters))
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
