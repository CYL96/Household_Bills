/**
 * One top-level expense category together with its children. The tree is
 * exactly two levels deep, so children are stored as plain names.
 */
export interface ExpenseCategoryGroup {
  readonly name: string
  readonly children: readonly string[]
}

/**
 * Built-in expense categories in display order. Every child belongs to the
 * group that declares it, and renaming a group never changes its children.
 */
export const EXPENSE_CATEGORY_TREE: readonly ExpenseCategoryGroup[] = [
  {
    name: '餐饮',
    children: [
      '堂食外卖',
      '蔬菜',
      '肉类',
      '柴米油盐',
      '水果',
      '奶茶咖啡',
      '咖啡豆',
      '酒水饮料',
      '甜品零食',
      '牛奶',
    ],
  },
  {
    name: '宝宝',
    children: [
      '奶粉',
      '尿不湿',
      '宝宝医疗',
      '服饰',
      '零食',
      '玩具',
      '宝宝娱乐',
      '宝宝学费',
      '宝宝书籍',
      '宝宝日用品',
    ],
  },
  {
    name: '购物',
    children: [
      '数码电器',
      '3D打印',
      '日用品',
      '家纺',
      '清洁用品',
      '厨具',
      '女装',
      '男装',
      '卫生巾',
      '零零碎碎',
    ],
  },
  {
    name: '住家',
    children: ['话费', '房贷', '物管', '电费', '水费', '燃气'],
  },
  {
    name: '医疗',
    children: ['门诊', '住院', '药品', '医保'],
  },
  {
    name: '交通',
    children: [
      '打车',
      '公共交通',
      '火车',
      '停车',
      '维修保养',
      '汽车保险',
      '飞机',
      '加油',
      '充电',
    ],
  },
  {
    name: '人情往来',
    children: ['孝心', '红包', '礼物'],
  },
  {
    name: '学习',
    children: ['学费', '书籍', '文具', '考试'],
  },
  {
    name: '美妆',
    children: ['洗面奶', '护肤品', '面膜', '首饰', '彩妆', '香水', 'SPA', '理发'],
  },
  {
    name: '娱乐',
    children: ['游戏', '运动', '聚会', '付费会员'],
  },
  {
    name: '旅游',
    children: ['酒店住宿', '门票', '旅行装备', '车费'],
  },
]

/**
 * Flat expense names derived from the tree, parents immediately followed by
 * their children. Kinds that only need a name set use this list.
 */
export const EXPENSE_CATEGORIES: readonly string[] =
  EXPENSE_CATEGORY_TREE.flatMap((group) => [group.name, ...group.children])

export const INCOME_CATEGORIES = [
  '工资',
  '奖金',
  '兼职收入',
  '理财收益',
  '报销',
  '红包',
  '其他收入',
]
