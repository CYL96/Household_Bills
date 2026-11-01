# 全屏记账与分类管理顶部留白修复

## 背景与目标

用户确认首页正常，只有进入新增支出、分类管理后顶部出现额外空白。两处全屏标题栏会读取 CSS `env(safe-area-inset-top)`，首页不读取顶部安全区；Android 壳层又已通过根布局 padding 避开状态栏。原生层继续向 WebView 传递未扣除的窗口 Insets，存在安全区被重复应用的问题。

[Chromium 官方 WebView Insets 指引](https://chromium.googlesource.com/chromium/src/+/refs/heads/main/android_webview/docs/insets.md?format=HTML)说明，WebView M144 起向非全屏 WebView 传递安全区；原生布局已通过 padding 处理的 Insets 应清零后继续分发，而非原样返回，也不应直接停止后代分发。

## 实际变更

- 修改 `android/app/src/main/java/com/household/bills/MainActivity.java` 的根布局 Insets 监听器。
- 保留原生状态栏、刘海、导航栏避让，以及 `max(导航栏高度, 键盘高度)` 的底部 padding。
- 将已经由原生 padding 处理的系统栏、刘海及键盘 Insets 清零，再交给 WebView，避免网页安全区或可视视口重复处理。
- 不删除两个页面的 CSS 安全区适配，不修改账目、分类或数据库逻辑。
- Android 版本更新为 `1.0.4`（`versionCode=5`），沿用现有正式签名，通过已有 `publishRelease` 流程发布安装包和 `latest.json`。

## 验证与结果

- Android Debug APK 编译通过：使用已安装的 Gradle 8.11.1、JDK 17、Android SDK 35，以离线模式和本机 `aapt2` 构建 `:app:assembleDebug`。初次 wrapper 下载已取消；未配置本机 `aapt2` 的离线构建因缺少 Maven 资源编译器失败，指定本机工具后通过。
- 在 Android 15 模拟器运行临时 Instrumentation，向真实根布局分发顶部 48、底部 24 的系统栏 Insets，并依次模拟键盘高度 `0 → 300 → 0`：
  - 修复前，子视图仍收到顶部 48、底部 24，断言失败。
  - 修复后，原生 padding 始终正确，子视图收到的已处理 Insets 全部为零，三个状态均通过。
- 初轮本地 Debug 构建与模拟器原应用签名不同，使用临时 applicationId 和独立 Provider authority 安装隔离验证包，未卸载原应用；随后找到项目现有正式签名，完成下述覆盖升级验证。
- 先在 WebView `124.0.6367.219` 检查旧运行时兼容性，再从 [Google 官方 SDK 系统镜像](https://dl.google.com/android/repository/sys-img/google_apis/x86_64-37.0_r08.zip)提取 Google 签名的 WebView，升级模拟器至 `145.0.7632.218`，重启应用后通过 User-Agent 确认新引擎已加载。
- 在 WebView 145 上直接复现与用户截图一致的空白：首页标题栏仍从 0 开始，原应用新增支出标题栏顶部 padding 为 59px、分类管理为 57px，均多出约 49px 的状态栏安全区。
- 在同一模拟器、同一 WebView 145、同一网页资源下打开隔离修复包并截图：新增支出标题栏顶部 padding 恢复为 10px，分类管理恢复为 8px；两个弹窗顶部坐标均为 0，首页标题栏仍贴顶，分类管理底部操作区可见。
- 实际打开分类编辑输入框、唤起和关闭软键盘：WebView 124 的网页高度由约 842px 缩至 530px 后恢复；WebView 145 由约 842px 缩至 529px 后恢复。输入框可见、弹窗贴顶，可视视口没有再次缩小一遍。
- 正式签名 Release 构建及 `:app:publishRelease` 离线执行成功；APK 包名为 `com.household.bills`，版本为 `1.0.4（5）`，签名证书 SHA-256 与已发布旧包一致。
- 本机与公网 `https://bill.bykkyy.top:888/api/app-update/` 均返回新版本；公网下载返回 HTTP 200，实际下载 51,932 字节，SHA-256 为 `0ad571509040d90deb9577478a2818b5b6626760a2d76a67cf5a06739110ba17`，大小及哈希与发布元数据一致。
- 在 Android 15 / WebView 145 模拟器中，从原正式版 `1.0.3（4）` 的“检查更新”入口发现 `1.0.4（5）`，完成下载、应用内校验、系统覆盖安装；系统显示安装成功，安装后包版本确认为 `1.0.4（5）`，原登录状态保留。
- 打开覆盖安装后的正式包并截图：首页正常，新增支出和分类管理标题栏均紧接状态栏下方，不再有额外顶部空白；分类管理底部操作区可见。未保存或修改账目、分类。
- 未运行前后端广泛回归或前端构建：本次没有改动前后端业务代码。

## 兼容性与交付

- 修改仅在 Android 11 / API 30 以上已有的 edge-to-edge 分支生效，旧版 Android 分支不变。
- 无数据迁移；浏览器端资源不变。
- 修复属于 APK 原生代码，仅刷新网页无法更新旧 APK 的 Insets 监听器。已发布同签名新版，用户可在应用设置中选择“检查更新”完成覆盖安装，无需卸载或迁移数据。
