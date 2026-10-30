import type { TransactionKind } from '../api/types'

export const EXPENSE_CATEGORIES = [
  '餐饮',
  '买菜',
  '日用百货',
  '交通出行',
  '住房房租',
  '水电燃气',
  '通讯网络',
  '医疗健康',
  '学习教育',
  '休闲娱乐',
  '服饰美容',
  '人情往来',
  '其他支出',
]

export const INCOME_CATEGORIES = [
  '工资',
  '奖金',
  '兼职收入',
  '理财收益',
  '报销',
  '红包',
  '其他收入',
]

export function categorySuggestions(kind: TransactionKind): string[] {
  if (kind === 'transfer') return []
  return kind === 'income' ? INCOME_CATEGORIES : EXPENSE_CATEGORIES
}
