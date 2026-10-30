import type { Component } from 'vue'
import {
  Apple as LucideApple,
  ArrowLeftRight as LucideArrowLeftRight,
  Baby as LucideBaby,
  Backpack as LucideBackpack,
  Balloon as LucideBalloon,
  Banknote as LucideBanknote,
  Bean as LucideBean,
  BedDouble as LucideBedDouble,
  Beef as LucideBeef,
  BookOpen as LucideBookOpen,
  Briefcase as LucideBriefcase,
  Brush as LucideBrush,
  Bubbles as LucideBubbles,
  Building as LucideBuilding,
  Bus as LucideBus,
  Candy as LucideCandy,
  Car as LucideCar,
  CarTaxiFront as LucideCarTaxiFront,
  Carrot as LucideCarrot,
  Coffee as LucideCoffee,
  Cookie as LucideCookie,
  CookingPot as LucideCookingPot,
  Crown as LucideCrown,
  Droplets as LucideDroplets,
  Dumbbell as LucideDumbbell,
  Ellipsis as LucideEllipsis,
  EvCharger as LucideEvCharger,
  FileCheck as LucideFileCheck,
  Flame as LucideFlame,
  Flower as LucideFlower,
  Fuel as LucideFuel,
  Gamepad2 as LucideGamepad2,
  Gem as LucideGem,
  Gift as LucideGift,
  HandCoins as LucideHandCoins,
  HandHeart as LucideHandHeart,
  HandPlatter as LucideHandPlatter,
  Handbag as LucideHandbag,
  Heart as LucideHeart,
  HeartHandshake as LucideHeartHandshake,
  HeartPulse as LucideHeartPulse,
  Hospital as LucideHospital,
  Hotel as LucideHotel,
  House as LucideHouse,
  Landmark as LucideLandmark,
  Library as LucideLibrary,
  Luggage as LucideLuggage,
  Milk as LucideMilk,
  Monitor as LucideMonitor,
  Package as LucidePackage,
  Palette as LucidePalette,
  PartyPopper as LucidePartyPopper,
  PencilRuler as LucidePencilRuler,
  PerfumeBottle as LucidePerfumeBottle,
  Pill as LucidePill,
  Plane as LucidePlane,
  Popcorn as LucidePopcorn,
  Printer3d as LucidePrinter3d,
  ReceiptText as LucideReceiptText,
  Route as LucideRoute,
  School as LucideSchool,
  Scissors as LucideScissors,
  ShieldCheck as LucideShieldCheck,
  Shirt as LucideShirt,
  ShoppingBag as LucideShoppingBag,
  Smartphone as LucideSmartphone,
  Sparkles as LucideSparkles,
  SprayCan as LucideSprayCan,
  SquareParking as LucideSquareParking,
  Star as LucideStar,
  Stethoscope as LucideStethoscope,
  Tag as LucideTag,
  Ticket as LucideTicket,
  ToyBrick as LucideToyBrick,
  TrainFront as LucideTrainFront,
  TrendingUp as LucideTrendingUp,
  Trophy as LucideTrophy,
  TubeLotion as LucideTubeLotion,
  UtensilsCrossed as LucideUtensilsCrossed,
  Venus as LucideVenus,
  Wallet as LucideWallet,
  Wheat as LucideWheat,
  Wine as LucideWine,
  Wrench as LucideWrench,
  Zap as LucideZap,
} from '@lucide/vue'

/** Picker groups in display order, matching the current category groups. */
export const CATEGORY_ICON_GROUPS = [
  { id: 'general', label: '通用' },
  { id: 'food', label: '餐饮' },
  { id: 'baby', label: '宝宝' },
  { id: 'shopping', label: '购物' },
  { id: 'home', label: '住家' },
  { id: 'medical', label: '医疗' },
  { id: 'transport', label: '交通' },
  { id: 'social', label: '人情往来' },
  { id: 'study', label: '学习' },
  { id: 'beauty', label: '美妆' },
  { id: 'entertainment', label: '娱乐' },
  { id: 'travel', label: '旅游' },
  { id: 'income', label: '收入' },
] as const satisfies readonly { readonly id: string; readonly label: string }[]

export type CategoryIconGroupId = (typeof CATEGORY_ICON_GROUPS)[number]['id']

/**
 * One selectable icon. `names` lists the built-in categories that use this
 * icon by default; the picker and the resolver share this declaration, so a
 * name is never maintained in a second mapping. Related categories can share
 * a selectable icon rather than duplicate the same drawing in the picker.
 */
export interface CategoryIconOption {
  readonly id: string
  readonly label: string
  readonly group: CategoryIconGroupId
  readonly component: Component
  readonly names: readonly string[]
}

/**
 * Curated icons for the current category tree. Ids are persisted with the
 * category preferences, so a retained option keeps its id.
 */
export const CATEGORY_ICON_OPTIONS = [
  // General
  { id: 'tag', names: [], label: '通用', group: 'general', component: LucideTag },
  { id: 'star', names: [], label: '特别', group: 'general', component: LucideStar },
  { id: 'heart', names: [], label: '爱心', group: 'general', component: LucideHeart },
  {
    id: 'other',
    names: ['零零碎碎'],
    label: '其他',
    group: 'general',
    component: LucideEllipsis,
  },

  // Food
  {
    id: 'lucide-utensils-crossed',
    names: ['餐饮'],
    label: '餐饮',
    group: 'food',
    component: LucideUtensilsCrossed,
  },
  {
    id: 'lucide-hand-platter',
    names: ['堂食外卖'],
    label: '堂食外卖',
    group: 'food',
    component: LucideHandPlatter,
  },
  {
    id: 'lucide-carrot',
    names: ['蔬菜'],
    label: '蔬菜',
    group: 'food',
    component: LucideCarrot,
  },
  { id: 'lucide-beef', names: ['肉类'], label: '肉类', group: 'food', component: LucideBeef },
  {
    id: 'lucide-wheat',
    names: ['柴米油盐'],
    label: '柴米油盐',
    group: 'food',
    component: LucideWheat,
  },
  {
    id: 'lucide-apple',
    names: ['水果'],
    label: '水果',
    group: 'food',
    component: LucideApple,
  },
  {
    id: 'lucide-coffee',
    names: ['奶茶咖啡'],
    label: '奶茶咖啡',
    group: 'food',
    component: LucideCoffee,
  },
  {
    id: 'lucide-bean',
    names: ['咖啡豆'],
    label: '咖啡豆',
    group: 'food',
    component: LucideBean,
  },
  {
    id: 'lucide-wine',
    names: ['酒水饮料'],
    label: '酒水饮料',
    group: 'food',
    component: LucideWine,
  },
  {
    id: 'lucide-cookie',
    names: ['甜品零食'],
    label: '甜品零食',
    group: 'food',
    component: LucideCookie,
  },
  {
    id: 'lucide-milk',
    names: ['牛奶', '奶粉'],
    label: '牛奶奶粉',
    group: 'food',
    component: LucideMilk,
  },

  // Baby
  {
    id: 'lucide-baby',
    names: ['宝宝', '尿不湿'],
    label: '宝宝尿不湿',
    group: 'baby',
    component: LucideBaby,
  },
  {
    id: 'lucide-candy',
    names: ['零食'],
    label: '宝宝零食',
    group: 'baby',
    component: LucideCandy,
  },
  {
    id: 'lucide-toy-brick',
    names: ['玩具'],
    label: '玩具',
    group: 'baby',
    component: LucideToyBrick,
  },
  {
    id: 'lucide-balloon',
    names: ['宝宝娱乐'],
    label: '宝宝娱乐',
    group: 'baby',
    component: LucideBalloon,
  },

  // Shopping
  {
    id: 'shopping-bag',
    names: ['购物'],
    label: '购物',
    group: 'shopping',
    component: LucideShoppingBag,
  },
  {
    id: 'monitor',
    names: ['数码电器'],
    label: '数码电器',
    group: 'shopping',
    component: LucideMonitor,
  },
  {
    id: 'lucide-printer-3d',
    names: ['3D打印'],
    label: '三维打印',
    group: 'shopping',
    component: LucidePrinter3d,
  },
  {
    id: 'lucide-package',
    names: ['日用品', '宝宝日用品'],
    label: '日用品',
    group: 'shopping',
    component: LucidePackage,
  },
  {
    id: 'lucide-bed-double',
    names: ['家纺'],
    label: '家纺',
    group: 'shopping',
    component: LucideBedDouble,
  },
  {
    id: 'lucide-spray-can',
    names: ['清洁用品'],
    label: '清洁用品',
    group: 'shopping',
    component: LucideSprayCan,
  },
  {
    id: 'lucide-cooking-pot',
    names: ['厨具'],
    label: '厨具',
    group: 'shopping',
    component: LucideCookingPot,
  },
  {
    id: 'lucide-handbag',
    names: ['女装'],
    label: '女装',
    group: 'shopping',
    component: LucideHandbag,
  },
  {
    id: 'lucide-shirt',
    names: ['男装', '服饰'],
    label: '男装服饰',
    group: 'shopping',
    component: LucideShirt,
  },
  {
    id: 'lucide-venus',
    names: ['卫生巾'],
    label: '卫生巾',
    group: 'shopping',
    component: LucideVenus,
  },

  // Home
  {
    id: 'lucide-house',
    names: ['住家'],
    label: '住家',
    group: 'home',
    component: LucideHouse,
  },
  {
    id: 'smartphone',
    names: ['话费'],
    label: '话费',
    group: 'home',
    component: LucideSmartphone,
  },
  {
    id: 'lucide-landmark',
    names: ['房贷'],
    label: '房贷',
    group: 'home',
    component: LucideLandmark,
  },
  {
    id: 'lucide-building',
    names: ['物管'],
    label: '物管',
    group: 'home',
    component: LucideBuilding,
  },
  { id: 'lucide-zap', names: ['电费'], label: '电费', group: 'home', component: LucideZap },
  {
    id: 'lucide-droplets',
    names: ['水费'],
    label: '水费',
    group: 'home',
    component: LucideDroplets,
  },
  {
    id: 'lucide-flame',
    names: ['燃气'],
    label: '燃气',
    group: 'home',
    component: LucideFlame,
  },

  // Medical
  {
    id: 'lucide-heart-pulse',
    names: ['医疗'],
    label: '医疗',
    group: 'medical',
    component: LucideHeartPulse,
  },
  {
    id: 'lucide-stethoscope',
    names: ['门诊', '宝宝医疗'],
    label: '门诊',
    group: 'medical',
    component: LucideStethoscope,
  },
  {
    id: 'lucide-hospital',
    names: ['住院'],
    label: '住院',
    group: 'medical',
    component: LucideHospital,
  },
  {
    id: 'lucide-pill',
    names: ['药品'],
    label: '药品',
    group: 'medical',
    component: LucidePill,
  },
  {
    id: 'lucide-shield-check',
    names: ['医保', '汽车保险'],
    label: '保险',
    group: 'medical',
    component: LucideShieldCheck,
  },

  // Transport
  {
    id: 'lucide-car',
    names: ['交通'],
    label: '交通',
    group: 'transport',
    component: LucideCar,
  },
  {
    id: 'lucide-car-taxi-front',
    names: ['打车'],
    label: '打车',
    group: 'transport',
    component: LucideCarTaxiFront,
  },
  {
    id: 'lucide-bus',
    names: ['公共交通'],
    label: '公共交通',
    group: 'transport',
    component: LucideBus,
  },
  {
    id: 'lucide-train-front',
    names: ['火车'],
    label: '火车',
    group: 'transport',
    component: LucideTrainFront,
  },
  {
    id: 'lucide-square-parking',
    names: ['停车'],
    label: '停车',
    group: 'transport',
    component: LucideSquareParking,
  },
  {
    id: 'lucide-wrench',
    names: ['维修保养'],
    label: '维修保养',
    group: 'transport',
    component: LucideWrench,
  },
  {
    id: 'lucide-plane',
    names: ['飞机'],
    label: '飞机',
    group: 'transport',
    component: LucidePlane,
  },
  {
    id: 'lucide-fuel',
    names: ['加油'],
    label: '加油',
    group: 'transport',
    component: LucideFuel,
  },
  {
    id: 'lucide-ev-charger',
    names: ['充电'],
    label: '充电',
    group: 'transport',
    component: LucideEvCharger,
  },

  // Social
  {
    id: 'lucide-heart-handshake',
    names: ['人情往来'],
    label: '人情往来',
    group: 'social',
    component: LucideHeartHandshake,
  },
  {
    id: 'lucide-hand-heart',
    names: ['孝心'],
    label: '孝心',
    group: 'social',
    component: LucideHandHeart,
  },
  {
    id: 'lucide-hand-coins',
    names: ['红包'],
    label: '红包',
    group: 'social',
    component: LucideHandCoins,
  },
  { id: 'gift', names: ['礼物'], label: '礼物', group: 'social', component: LucideGift },

  // Study
  {
    id: 'lucide-library',
    names: ['学习'],
    label: '学习',
    group: 'study',
    component: LucideLibrary,
  },
  {
    id: 'school',
    names: ['学费', '宝宝学费'],
    label: '学费',
    group: 'study',
    component: LucideSchool,
  },
  {
    id: 'lucide-book-open',
    names: ['书籍', '宝宝书籍'],
    label: '书籍',
    group: 'study',
    component: LucideBookOpen,
  },
  {
    id: 'lucide-pencil-ruler',
    names: ['文具'],
    label: '文具',
    group: 'study',
    component: LucidePencilRuler,
  },
  {
    id: 'lucide-file-check',
    names: ['考试'],
    label: '考试',
    group: 'study',
    component: LucideFileCheck,
  },

  // Beauty
  {
    id: 'lucide-brush',
    names: ['美妆'],
    label: '美妆',
    group: 'beauty',
    component: LucideBrush,
  },
  {
    id: 'lucide-bubbles',
    names: ['洗面奶'],
    label: '洗面奶',
    group: 'beauty',
    component: LucideBubbles,
  },
  {
    id: 'lucide-tube-lotion',
    names: ['护肤品'],
    label: '护肤品',
    group: 'beauty',
    component: LucideTubeLotion,
  },
  {
    id: 'lucide-sparkles',
    names: ['面膜'],
    label: '面膜',
    group: 'beauty',
    component: LucideSparkles,
  },
  {
    id: 'lucide-gem',
    names: ['首饰'],
    label: '首饰',
    group: 'beauty',
    component: LucideGem,
  },
  {
    id: 'lucide-palette',
    names: ['彩妆'],
    label: '彩妆',
    group: 'beauty',
    component: LucidePalette,
  },
  {
    id: 'lucide-perfume-bottle',
    names: ['香水'],
    label: '香水',
    group: 'beauty',
    component: LucidePerfumeBottle,
  },
  {
    id: 'lucide-flower',
    names: ['SPA'],
    label: '水疗',
    group: 'beauty',
    component: LucideFlower,
  },
  {
    id: 'lucide-scissors',
    names: ['理发'],
    label: '理发',
    group: 'beauty',
    component: LucideScissors,
  },

  // Entertainment
  {
    id: 'lucide-popcorn',
    names: ['娱乐'],
    label: '娱乐',
    group: 'entertainment',
    component: LucidePopcorn,
  },
  {
    id: 'lucide-gamepad-2',
    names: ['游戏'],
    label: '游戏',
    group: 'entertainment',
    component: LucideGamepad2,
  },
  {
    id: 'lucide-dumbbell',
    names: ['运动'],
    label: '运动',
    group: 'entertainment',
    component: LucideDumbbell,
  },
  {
    id: 'lucide-party-popper',
    names: ['聚会'],
    label: '聚会',
    group: 'entertainment',
    component: LucidePartyPopper,
  },
  {
    id: 'lucide-crown',
    names: ['付费会员'],
    label: '付费会员',
    group: 'entertainment',
    component: LucideCrown,
  },

  // Travel
  {
    id: 'lucide-luggage',
    names: ['旅游'],
    label: '旅游',
    group: 'travel',
    component: LucideLuggage,
  },
  {
    id: 'lucide-hotel',
    names: ['酒店住宿'],
    label: '酒店住宿',
    group: 'travel',
    component: LucideHotel,
  },
  { id: 'ticket', names: ['门票'], label: '门票', group: 'travel', component: LucideTicket },
  {
    id: 'lucide-backpack',
    names: ['旅行装备'],
    label: '旅行装备',
    group: 'travel',
    component: LucideBackpack,
  },
  {
    id: 'lucide-route',
    names: ['车费'],
    label: '车费',
    group: 'travel',
    component: LucideRoute,
  },

  // Income
  {
    id: 'lucide-banknote',
    names: ['工资'],
    label: '工资',
    group: 'income',
    component: LucideBanknote,
  },
  {
    id: 'lucide-trophy',
    names: ['奖金'],
    label: '奖金',
    group: 'income',
    component: LucideTrophy,
  },
  {
    id: 'lucide-briefcase',
    names: ['兼职收入'],
    label: '兼职收入',
    group: 'income',
    component: LucideBriefcase,
  },
  {
    id: 'lucide-trending-up',
    names: ['理财收益'],
    label: '理财收益',
    group: 'income',
    component: LucideTrendingUp,
  },
  {
    id: 'lucide-receipt-text',
    names: ['报销'],
    label: '报销',
    group: 'income',
    component: LucideReceiptText,
  },
  {
    id: 'wallet',
    names: ['其他收入'],
    label: '其他收入',
    group: 'income',
    component: LucideWallet,
  },
  {
    id: 'lucide-arrow-left-right',
    names: ['账户转账'],
    label: '账户转账',
    group: 'income',
    component: LucideArrowLeftRight,
  },
] as const satisfies readonly CategoryIconOption[]

export type CategoryIconId = (typeof CATEGORY_ICON_OPTIONS)[number]['id']


export const DEFAULT_CATEGORY_ICON: CategoryIconId = 'tag'
export const DEFAULT_CATEGORY_ICON_GROUP: CategoryIconGroupId = 'general'

/** Narrows persisted values to the curated icon allow-list. */
export function isCategoryIconId(value: unknown): value is CategoryIconId {
  return typeof value === 'string' && CATEGORY_ICON_COMPONENTS.has(value)
}

/** Built-in category name to icon id, resolved once at module load. */
const CATEGORY_ICON_BY_NAME = new Map<string, CategoryIconId>()
for (const option of CATEGORY_ICON_OPTIONS) {
  for (const name of option.names) {
    CATEGORY_ICON_BY_NAME.set(name, option.id)
  }
}

/** Id to component, so rendering never scans the catalog. */
const CATEGORY_ICON_COMPONENTS = new Map<string, Component>()
for (const option of CATEGORY_ICON_OPTIONS) {
  CATEGORY_ICON_COMPONENTS.set(option.id, option.component)
}

/**
 * Effective icon id: a valid explicit choice wins, then the built-in name,
 * then the generic tag. Map lookups keep names such as `constructor` or
 * `__proto__` from matching inherited object properties.
 */
export function categoryIconId(
  category: string,
  icon?: CategoryIconId,
): CategoryIconId {
  if (isCategoryIconId(icon)) return icon
  return CATEGORY_ICON_BY_NAME.get(category) ?? DEFAULT_CATEGORY_ICON
}

/** Renders the same curated component that the picker offers for the id. */
export function categoryIcon(
  category: string,
  icon?: CategoryIconId,
): Component {
  return (
    CATEGORY_ICON_COMPONENTS.get(categoryIconId(category, icon)) ?? LucideTag
  )
}
