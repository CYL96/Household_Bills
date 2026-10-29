import { request } from './client'
import type { Summary } from './types'

export function getSummary(month: string): Promise<Summary> {
  return request<Summary>(`/api/summary/?month=${encodeURIComponent(month)}`)
}
