# 日期选择控件避免唤起 App 软键盘

## 背景与目标

用户反馈 Android App 点击时间筛选及新增流水的记账时间时，会弹出软键盘。日期、月份、时间应通过选择面板操作，不应触发文字输入；备注、搜索和金额输入保持原有行为。

## 实际变更

四处 Element Plus `el-date-picker` 统一增加 `:editable="false"`：

- `frontend/src/views/DashboardView.vue`：总览汇总月份。
- `frontend/src/views/ReportsView.vue`：统计报表月份。
- `frontend/src/views/TransactionsView.vue`：流水月份筛选，保留原有清空能力。
- `frontend/src/components/TransactionDialog.vue`：新增、编辑共用的记账日期时间。

当前安装的 Element Plus 2.14.7 默认允许日期文字输入。关闭 `editable` 后，控件生成只读输入框，同时将该设置传给日期时间弹层内的日期、时间输入框；日历格、时间滚轮、确定和清空操作仍可用。未使用会限制面板操作的 `readonly` 组件属性，未添加全局键盘拦截或 Android 特判。

## 验证与结果

- 执行 `npm run build -- --outDir /tmp/household-date-keyboard-dist`，TypeScript 检查和生产构建通过。构建仍提示部分分包超过 500 kB，未调整无关构建策略。
- 使用独立 PostgreSQL 临时数据库、现有后端二进制及 Chromium，验证生产构建页面；未修改已有账本数据。
- 390×844 移动视口下，总览、统计、流水三个外层月份输入均为 DOM `readOnly === true`；点击后仍打开月份面板，可从十一月切换到十月。流水月份清空后显示“全部月份流水”。
- 新增流水的记账时间外层输入及弹层内日期、时间两个输入均为 `readOnly === true`。实际点击日历选择 11 月 4 日，通过时间滚轮把小时改为 10，确认后得到 `2026-11-04 10:58:45`；填写金额和备注后创建成功，真实接口读回日期为 `2026-11-04T10:58:45`。
- 桌面视口打开已有流水编辑窗口，确认记账时间为只读；通过日历将日期从 11 月 3 日改为 11 月 5 日并保存，接口读回 `2026-11-05T09:00:00`，原时间部分保持不变。
- 确认备注、金额和搜索输入的 DOM `readOnly` 仍为 `false`；备注和搜索可填写，金额原有 `inputmode="none"` 未变。
- 未运行后端测试或广泛回归测试：本次仅修改四个前端控件属性，已用真实页面与接口做针对性验证。
- **未执行 Android 真机或模拟器验证**：环境未提供可用的 adb、emulator。浏览器验证证明只读属性和选择、提交功能正常，但不能直接证明具体 Android WebView／输入法的软键盘状态；禁止日期文本编辑可避免其触发文字输入键盘属于基于标准输入框行为的推断。

## 兼容性与部署

- 所有端统一使用面板选择日期时间，桌面端也不再支持手工键入日期文本；不影响日历、时间滚轮及原有清空规则。
- 无后端接口、数据库迁移、Android 原生代码或 APK 变更；重新构建、部署前端后，现有 App 加载新页面即可使用。
- 根 README 不变：本次为输入行为修复，未新增用户功能或变更部署步骤。

## 现有服务部署记录

- 已按要求部署到现有用户级 `household-bills.service`，地址为 `http://127.0.0.1:9890`，静态资源目录为 `frontend/dist`。
- 重新构建前端到临时目录，先备份旧 `dist`，再复制新版本的哈希资源，最后原子替换 `index.html`。保留旧资源，避免已打开的页面继续请求旧分包时出现 404。
- 旧版本备份位置：`/home/nixos/.local/state/household-bills/deployments/20261103-date-picker-keyboard/dist`。需要回退时，可把其中的 `index.html` 通过临时文件复制后原子替换回现有 `frontend/dist/index.html`；旧资源仍在现有目录中。
- 现有服务按请求读取静态文件，因此本次未重启后端，未修改数据库、连接配置或 Android APK。部署后服务仍为 `active/running`，主进程保持 PID 1590。
- HTTP 检查确认入口返回 200、`Cache-Control: no-cache`；入口从 `index-C1A503Ta.js` 切换到 `index-C6OgUMlk.js`。逐一请求新构建的 33 个文件，均返回 200，内容哈希与构建产物一致；旧入口分包仍返回 200。
- 现有 Nginx HTTPS 入口 `https://bill.bykkyy.top:888/` 检查返回 HTTP/2 200、`Cache-Control: no-cache`，未修改代理配置。
- Chromium 实际访问现有服务，正常跳转并显示登录页，新入口、登录页分包、样式和匿名会话接口均返回 200，无页面运行错误。未登录或修改现有账本；日期选择功能沿用前述独立临时账本验证，Android 真机验证仍未执行。
- App 需重新加载服务器页面才能使用新版本；已打开的页面不会被强制刷新。
