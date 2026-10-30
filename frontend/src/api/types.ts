export type TransactionKind = 'income' | 'expense' | 'transfer'

export interface User {
  id: number
  username: string
  nickname: string
  is_admin: boolean
  avatar_version: number
}

export interface Household {
  id: number
  name: string
}

export interface MeResponse {
  user: User | null
  household?: Household | null
  members?: User[]
}

export interface LoginPayload {
  username: string
  password: string
}

export interface CreateUserPayload {
  username: string
  nickname?: string
  password: string
}

export interface UpdateUserPayload {
  username?: string
  nickname?: string
  password?: string
  current_password?: string
}

export interface Account {
  id: number
  name: string
  opening_balance: string
  balance: string
  owner: number | null
  owner_name: string
}

export interface AccountPayload {
  name: string
  opening_balance: string
  /**
   * 新建时省略即为公共账户；修改时省略保持原归属。
   * null 明确表示公共账户，数字表示现有非管理员家庭成员的用户 ID。
   */
  owner?: number | null
}

export interface Transaction {
  id: number
  account: number
  account_name: string
  kind: TransactionKind
  amount: string
  category: string
  /**
   * Local civil date-time in YYYY-MM-DDTHH:mm:ss (no timezone offset).
   * Includes seconds precise to the recorded transaction time.
   */
  date: string
  note: string
  created_by: number
  created_by_name: string
  /** Destination account for transfers; always null for income/expense. */
  transfer_account: number | null
  transfer_account_name: string
}

export interface TransactionPayload {
  account: number
  kind: TransactionKind
  amount: string
  category: string
  /** Local civil date-time in YYYY-MM-DDTHH:mm:ss (no timezone offset). */
  date: string
  note?: string
  /** Destination account for transfers; send null to clear it. */
  transfer_account?: number | null
}

export interface TransactionFilters {
  month?: string
  account?: number
  kind?: TransactionKind
  q?: string
}

export interface SummaryCategory {
  category: string
  amount: string
}

export interface SummaryDay {
  /** Calendar date in YYYY-MM-DD; all transactions of that local day are merged. */
  date: string
  income: string
  expense: string
}

export interface Summary {
  month: string
  income: string
  expense: string
  net: string
  total_balance: string
  categories: SummaryCategory[]
  daily: SummaryDay[]
}
