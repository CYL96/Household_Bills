-- Rebuild expense choices once; keep income settings and historical bills intact.
-- Increment the revision so an already-open settings draft cannot restore the old set.
WITH category_groups (position, name, children) AS (
    VALUES
        (1, '餐饮', ARRAY['堂食外卖', '蔬菜', '肉类', '柴米油盐', '水果', '奶茶咖啡', '咖啡豆', '酒水饮料', '甜品零食', '牛奶']),
        (2, '宝宝', ARRAY['奶粉', '尿不湿', '宝宝医疗', '服饰', '零食', '玩具', '宝宝娱乐', '宝宝学费', '宝宝书籍', '宝宝日用品']),
        (3, '购物', ARRAY['数码电器', '3D打印', '日用品', '家纺', '清洁用品', '厨具', '女装', '男装', '卫生巾', '零零碎碎']),
        (4, '住家', ARRAY['话费', '房贷', '物管', '电费', '水费', '燃气']),
        (5, '医疗', ARRAY['门诊', '住院', '药品', '医保']),
        (6, '交通', ARRAY['打车', '公共交通', '火车', '停车', '维修保养', '汽车保险', '飞机', '加油', '充电']),
        (7, '人情往来', ARRAY['孝心', '红包', '礼物']),
        (8, '学习', ARRAY['学费', '书籍', '文具', '考试']),
        (9, '美妆', ARRAY['洗面奶', '护肤品', '面膜', '首饰', '彩妆', '香水', 'SPA', '理发']),
        (10, '娱乐', ARRAY['游戏', '运动', '聚会', '付费会员']),
        (11, '旅游', ARRAY['酒店住宿', '门票', '旅行装备', '车费'])
), entries AS (
    SELECT position, 0 AS child_position,
        jsonb_build_object('name', name, 'visible', true, 'default_name', name) AS entry
    FROM category_groups
    UNION ALL
    SELECT groups.position, child.position::integer,
        jsonb_build_object(
            'name', child.name, 'visible', true,
            'default_name', child.name, 'parent_name', groups.name
        ) AS entry
    FROM category_groups AS groups
    CROSS JOIN LATERAL unnest(groups.children) WITH ORDINALITY AS child(name, position)
), preferences AS (
    SELECT jsonb_build_object(
        'entries', jsonb_agg(entry ORDER BY position, child_position),
        'deleted_defaults', '[]'::jsonb
    ) AS expense
    FROM entries
)
UPDATE household_category_settings AS settings
SET expense = preferences.expense,
    revision = settings.revision + 1,
    updated_at = now()
FROM preferences;
