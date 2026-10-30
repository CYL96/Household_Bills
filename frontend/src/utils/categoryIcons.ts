import type { Component } from 'vue'
import {
  Bowl,
  Briefcase,
  Cellphone,
  CoffeeCup,
  FirstAidKit,
  Goods,
  Handbag,
  House,
  Lightning,
  Money,
  MoreFilled,
  Present,
  PriceTag,
  Reading,
  ShoppingTrolley,
  Switch as SwitchIcon,
  Tickets,
  TrendCharts,
  Trophy,
  Van,
} from '@element-plus/icons-vue'

/**
 * Shared between the accounting entry page and the category settings screen.
 * The own-property check below keeps custom names such as `constructor` or
 * `__proto__` from matching inherited Object properties.
 */
const CATEGORY_ICONS: Record<string, Component> = {
  餐饮: Bowl,
  买菜: ShoppingTrolley,
  日用百货: Goods,
  交通出行: Van,
  住房房租: House,
  水电燃气: Lightning,
  通讯网络: Cellphone,
  医疗健康: FirstAidKit,
  学习教育: Reading,
  休闲娱乐: CoffeeCup,
  服饰美容: Handbag,
  人情往来: Present,
  其他支出: MoreFilled,
  工资: Money,
  奖金: Trophy,
  兼职收入: Briefcase,
  理财收益: TrendCharts,
  报销: Tickets,
  红包: Present,
  其他收入: MoreFilled,
  账户转账: SwitchIcon,
}

/** Icon for a configured or historical category, falling back to a tag. */
export function categoryIcon(category: string): Component {
  if (!Object.prototype.hasOwnProperty.call(CATEGORY_ICONS, category)) {
    return PriceTag
  }
  return CATEGORY_ICONS[category] ?? PriceTag
}
