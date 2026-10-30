import type { User } from '../api/types'

/** 昵称优先，留空时回退到登录用户名。 */
export function displayName(user: Pick<User, 'username' | 'nickname'>): string {
  return user.nickname?.trim() || user.username
}

/** 头像首字，取显示名的第一个字符。 */
export function displayInitial(user: Pick<User, 'username' | 'nickname'>): string {
  return [...displayName(user)][0]?.toUpperCase() ?? ''
}
