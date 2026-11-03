# App 内网页自动更新与编辑保护

## 背景与目标

网页升级后，已打开的 App WebView 会继续运行旧 SPA，用户需要手动刷新页面缓存。改为自动检查服务端入口版本，在不会丢失输入和中断提交的安全时机加载新版；保留登录状态和当前完整 URL，不以清空缓存解决更新问题。

## 实际变更

- Vite 每次生产构建生成一个构建标识，同时注入 HTML 的 `hb-build-id` meta 和 JavaScript 常量。检测读取同一份 HTML，不另建版本 JSON 或版本 API，避免两个文件发布不同步。开发模式不开启检测。
- 新增 `frontend/src/composables/webUpdates.ts`：启动、网页重新可见、联网恢复、原生回前台时检查；前台每两分钟补充检查。请求使用同源根入口、`cache: no-store`、8 秒超时和单飞保护；周期请求节流，前台恢复立即检查。失败清除待更新目标，保留当前页面，不弹错误或自动重试。
- 新版本先等待：编辑保护全部释放后，经 Vue 渲染和下一宏任务再次检查可见性、网络与保护状态，再 `location.reload()`。待更新目标超过 30 秒会先重验；新的探测开始时清除旧目标，避免用户关闭表单时使用过期结果刷新。
- 启动异步操作前即安装原生 pause/resume 监听；认证恢复、路由准备和应用挂载期间持有启动保护，避免漏掉早到的原生暂停事件。
- 记账、账户、新建成员、个人资料/密码/头像、分类草稿与分类编辑器、登录输入均接入保护。保存、上传、删除、注销的忙碌状态继续保护组件收尾；共享 API 客户端统一保护写请求的 CSRF 准备、请求及响应处理，确认框在整个等待期间也受保护。导航菜单、报表/流水/日历筛选和只读加载不作为未保存编辑。
- sessionStorage 只记录已自动刷新过的目标构建，阻止异常缓存导致同目标反复刷新；写入受限时不自动刷新，保留手动重新加载方式。不保存登录凭据或表单草稿，不清 Cookie、localStorage 或 WebView 资源缓存。
- Android `MainActivity` 通过标准 `household-bills:pause` / `household-bills:resume` 事件通知网页前后台、原生设置/错误页遮挡状态。复用集中页面切换入口，同源校验并按文档重置通知状态；不新增 JavaScript 原生桥，不原生强制刷新。版本递增为 **1.0.6（versionCode 7）**。
- 修复后端静态缓存分类：`/`、SPA 路由、直接 `/index.html` 与非哈希文件需重验证；仅 `/assets/` 下带 Vite 内容哈希的资源长期 immutable。保留未知 API 和缺失资源不回落 SPA 的行为，新增 HTTP 缓存契约回归测试。
- 更新 README 的必要使用说明及 `docs/development.md` 的自动更新和安全部署说明。

## 验证记录

使用独立 PostgreSQL 17 实例、独立 Go 服务、临时构建与发布目录、独立 Chromium 配置和 API 35 Android 模拟器；没有使用线上账本执行写操作，没有覆盖线上 `frontend/dist`。

### 编译与聚焦测试

- `go test ./internal/api -run 'TestStaticCacheControl|TestSPAFallbackAndAPI404' -count=1`：通过；另构建隔离后端成功。对变更 Go 文件执行 `gofmt`。
- `npm run build -- --outDir <临时目录>`：类型检查和生产构建通过；另生成第二份生产构建用于真实版本切换，HTML meta 与入口 JS 中的标识一致。Vite 仍提示部分分块超过 500 kB，不影响构建。
- `nix-shell --run 'android/gradlew -p android --offline --no-daemon --console=plain :app:assembleDebug'`：通过，安装 Debug APK 到独立 API 35 模拟器成功。
- 实际 Go 服务 HTTP 响应：`/`、`/index.html`、带 query 的 `/transactions` 为 `no-cache`；哈希入口 JS 为 `public, max-age=31536000, immutable`。

### 真实浏览器与 WebView 场景

- 服务端原子替换入口后，空闲页面自动加载新构建，保留完整路径/query/hash、登录 Cookie 和 localStorage；身份仍有效，未回到登录页。
- 打开记账输入金额 `123` 和备注，再切换服务端构建并触发检查：文档、金额和备注保持不变；关闭表单后完成延后更新，完整 URL 不变。
- 延迟真实账户创建 POST，期间触发检查：提交未被刷新中断；放行请求后账户创建成功，延后更新完成，新页面可见该账户。
- 登录页填写账号密码期间发现新版：不刷新；提交真实登录后恢复目标深链接并完成延后更新。
- 已发现更新但编辑尚未关闭时，让下一次入口检查网络失败：关闭编辑后仍保留可用旧页面，不按旧目标盲目刷新；恢复网络并触发 online 后更新成功。
- 已尝试过同一目标构建时不再次自动刷新；模拟 sessionStorage 写入失败时保留当前文档，不循环刷新。
- API 35 App 真正进入后台后切换服务端构建，再恢复 Activity：自动更新到新版，保留已登录的流水深链接及 query/hash，并完成截图观察。
- 从网页实际「应用设置」入口打开原生设置，切换构建并触发 online：隐藏网页保持原文档；原生返回键退出设置后完成延后更新。

验证中发现并修复了 WebView 时序问题：原生 resume 可能先于 `document.visibilityState` 恢复，若可见性事件仍受 30 秒节流，会遗漏本次立即检查。修复为重新可见和联网恢复立即检查、保留单飞保护后，重新构建并通过上述真实 Activity 恢复场景。

实现阶段未运行全量回归、Docker 构建、所有表单逐项实机操作或真机测试；正式 APK 后续发布见下文，未部署线上网页或后端。已验证的写请求延迟是浏览器层暂缓后继续发送到真实后端，不是模拟 API 成功响应。

## 兼容与部署

- 无数据库迁移，无新增 API。HTML 缓存修复需要更新后端二进制；APK 生命周期通知需要安装 1.0.6，旧 APK 仍可使用网页自身的可见性和前台周期检查。
- 首次部署本机制时，已经打开的旧网页没有检测代码，需手动刷新一次。以后升级使用自动检测；原有「刷新页面缓存」保留为异常兜底。
- 在临时 outDir 构建，先复制新资源并保留旧哈希资源，再原子替换同一构建的 `index.html`；不要在正在服务的 `frontend/dist` 原地执行会清空目录的构建。
- 只保证当前完整 URL 不变，不持久化仅在内存中的筛选、滚动或已经取消的草稿。登录有效期仍遵循既有认证策略。
- 初次交付完成源码、文档、Debug 构建与隔离运行验证；随后按用户要求发布正式 APK，未切换线上网页或后端。

## 正式 APK 发布（2026-11-03）

- 使用既有正式签名执行 `nix-shell --run 'android/gradlew -p android --offline --no-daemon --console=plain :app:publishRelease'`，构建与发布成功，原子更新 `android/release/latest.json`。
- 发布版本：**1.0.6（versionCode 7）**，最低 Android API 26；APK 大小 **52,772 字节**，未启用 debuggable。
- APK：`android/release/household-bills-7-850a0c0d20c675c57082024e851763bdfcb3f785453dcc890384bb190f05f46b.apk`。
- SHA-256：`850a0c0d20c675c57082024e851763bdfcb3f785453dcc890384bb190f05f46b`。
- `apksigner verify --verbose --print-certs` 验证 v2 签名通过；签名证书 SHA-256 为 `4aedbbd0ea296d721d1263120fb78765bd30fd1ee8ce47c2eaad07843c6f8658`，与上一正式版本及既有签名密钥一致，可用于覆盖升级。发布前后均未生成新签名密钥。
- 本机 `http://127.0.0.1:9890/api/app-update/` 与公网 `https://bill.bykkyy.top:888/api/app-update/` 均返回 1.0.6 / 7；两处完整 APK 下载的字节数与 SHA-256 均匹配发布清单。
- 未重复执行正式 APK 安装与交互测试；本任务前面的 API 35 行为验证使用 Debug APK，不能等同于正式包安装验证。
- 本次仅发布 APK，未部署网页或更新后端二进制。发布后读取公网根入口，尚无 `hb-build-id` 标记；网页自动检测和编辑保护需部署本任务的网页代码后才会生效，不能只升级 APK。
