# Android 从当前服务器检查并安装 APK 更新

## 背景与目标

既有 WebView 客户端已能配置服务器和刷新网页缓存，但原生 APK 更新还需要手动分发。本次增加原生「检查更新」，使用当前已保存的账本服务器作为唯一更新源，完成查询、下载、验证和 Android 系统覆盖安装，并提供服务器发布方式。

此需求不改变账本登录、家庭隔离、业务接口、数据库结构或前端 UI。原有 APK 与缓存刷新历史见 [Android WebView 客户端记录](2026-10-31-android-webview-apk.md)。

## 实际变更

### Android 客户端

- 版本升级为 `1.0.2` / `versionCode=3`，包名仍为 `com.household.bills`，最低 Android 8.0（API 26），目标 API 35；沿用原签名，无新增第三方运行时依赖。
- `MainActivity.java` 的原生设置页新增「应用更新」区块，显示当前版本、服务器版本、检查/下载/安装/重试操作、下载进度和取消。区块位于页面缓存区下方，可滚动访问；没有服务器时隐藏，不覆盖 WebView。
- 新 `AppUpdater.java` 使用后台线程和有限连接/读取超时访问已保存的 `committedOrigin`，不使用地址输入框草稿、不发送 WebView Cookie、不跟随 HTTP 重定向，HTTPS 保留系统证书验证。状态和错误保留在设置页，不替换账本页面。
- 元数据最多 16 KiB，APK 最多 512 MiB；流式下载至应用私有缓存，同时计算 SHA-256。校验实际大小、哈希、包名、版本号/版本名称、最低系统版本以及与已安装应用一致的签名证书集合；拒绝降级和错误签名。
- 发现新版先确认下载，校验通过后再确认安装。新增 `REQUEST_INSTALL_PACKAGES`；首次可跳转系统「允许来自此来源的应用」，返回后重新验证 APK，再启动系统安装器。安装最终由用户在系统界面确认，不静默安装。
- 新 `UpdateFileProvider.java` 非导出，仅通过临时 URI 读取授权向安装器提供固定私有 APK，禁止写入、删除和其它路径访问，不申请公共存储权限。
- 退出设置、切换服务器和 Activity 销毁会取消请求并使旧回调失效；旋转等重建可重新检查，不做后台断点续传。临时下载使用独立随机文件名，避免不同 Activity 的局部 generation 重复导致临时文件互相删除。交给安装器的已下载文件不会因普通 `onPause` / `onDestroy` 被提前删除。
- 保留已有服务器配置、WebView 安全边界、刷新页面缓存与切站清理流程。

### 后端接口

- 新增 `Config.AndroidReleaseDir`，从 `ANDROID_RELEASE_DIR` 读取，默认 `../android/release`，相对 Go 进程工作目录。
- 新 `backend/internal/api/app_updates.go` 提供公开、无需登录的更新接口，路由独立于 session/CSRF 中间件，因此检查 APK 不依赖数据库会话：
  - `GET /api/app-update/`：未发布返回 `{"available":false}`；发布后返回 `available`、`version_code`、`version_name`、`min_sdk`、`size_bytes`、`sha256`、相对 `download_path`。
  - `GET/HEAD /api/app-update/download/<sha256>/`：只下载当前清单指定 APK；旧/错误哈希返回 JSON 404，支持标准 Range 和 HEAD。
- 每次读取 `latest.json`，发布后无需重启服务。清单限制 16 KiB，严格检查字段、版本/大小范围、安全文件名及被引用文件的类型/大小；存在但损坏的发布返回泛化 500，而不是伪装成无更新。
- 使用 `os.OpenRoot` 限定发布目录，防止路径穿越及符号链接逃逸；APK 不进入前端静态目录，更新 API 404/405 不回落 SPA。
- 响应使用 `Cache-Control: no-store`；下载添加正确 APK MIME、attachment 与 `nosniff`，局部延长写超时至有限的 10 分钟，避免全局 60 秒限制截断慢速下载。
- 实测发现 `http.ServeContent` 会在 416 错误时移除缓存头；使用轻量响应 Writer 在写状态码时恢复 `no-store` / `nosniff`，保留标准 Range 处理，并增加越界 Range 回归断言。
- 服务端不逐次对整个 APK 计算哈希；发布器根据真实 APK 生成哈希、大小及不可变文件名，客户端对下载内容独立验证。管理员发布内容不通过 HTTP 上传。

### 构建与发布

- `android/app/build.gradle` 新增 `:app:publishRelease`，依赖正式 Release 构建，缺少完整签名环境或无签名输出时拒绝发布。保留原有 JUnit 4 依赖与地址测试。
- 版本字段在任务执行时读取最终 Gradle DSL；流式复制并计算真实 APK 的大小和 SHA-256，先原子发布 `household-bills-<versionCode>-<sha256>.apk`，再原子替换六字段 `latest.json`。不手工维护下载地址或哈希，不把半写入包作为最新版本提供。
- 默认发布目录为 `android/release/`。Gradle 的 `ANDROID_RELEASE_DIR` 自定义相对值以 `android/` 工程根为基准；后端同名变量相对进程工作目录，分机/自定义部署建议使用绝对路径。
- 发布目录和文件设置为服务用户可读；Compose 只读挂载 `${ANDROID_RELEASE_HOST_DIR:-./android/release}` 到容器 `/app/android-release`，Docker 镜像内 `ANDROID_RELEASE_DIR` 指向该路径。`.env.example` 区分宿主挂载变量与进程发布变量。
- Git/Docker 构建上下文排除 APK 发布物、Android 构建产物及签名私钥。README 更新原生更新操作、签名发布命令、路径语义、反向代理和覆盖升级规则。

## 实际验证

### 构建、接口与签名

- Go 1.26.10：运行 `go test ./internal/api -run '^TestAppUpdate' -count=1 -v`，3 个顶层更新接口测试通过，覆盖未发布、元数据、完整下载、206 Range、HEAD、416 边界、错误哈希、损坏/尾随清单、包大小不符、路径穿越与符号链接逃逸。测试使用临时目录和无数据库 HTTP 服务，没有截断业务表。
- 构建真实 Go 后端二进制与隔离 HTTP 烟测程序成功；真实公开查询在携带无效 session Cookie、且无数据库 Store 的情况下仍返回 200 未发布，证明更新路由不受会话中间件影响。
- JDK 17 / Gradle 8.11.1 / Android SDK 35：`:app:testReleaseUnitTest :app:assembleRelease` 成功，现有 4 项地址测试失败 0、错误 0、跳过 0，Release 附带 lint vital 检查通过。
- `:app:publishRelease` 实际生成正式 APK 和 `latest.json`；服务由未发布立即变为可查询版本，发布 APK 本身不需要重启。APK Signature Scheme v2 验证通过，签名证书与之前交付相同；`aapt` 确认版本、SDK 及仅有 INTERNET / REQUEST_INSTALL_PACKAGES 两项权限，ADB `run-as` 拒绝访问不可调试 Release 包。

### Android 15 实际更新安装

使用 API 35 AOSP x86_64 模拟器，通过 ADB 操作原生页面、系统授权和安装器，观察 UI 层级及截图：

1. 从已配置服务器的 1.0.1 覆盖安装 1.0.2，服务器配置保留；设置页显示当前版本和更新入口，未发布时明确展示服务器尚未发布。
2. 正式服务器发布 1.0.2 后，App 检查显示当前已是最新版本。
3. 用隔离构建目录和临时 Gradle init 脚本生成真实同签名 code 4 / `1.0.3-smoke` APK，发布到隔离 Go 更新服务器；未覆盖正式发布目录或正式版本号。
4. 故意让发布哈希与文件不符，客户端拒绝 SHA-256 不匹配；使用同包名/同版本但不同签名的真实 Debug APK，客户端拒绝签名不一致，均未提供安装确认。
5. 恢复正确签名包后，依次经过发现新版、下载、校验、安装确认、系统未知来源设置。先不授权返回，显示可继续安装；再次进入并授权后，重新校验并打开系统「Do you want to update this app?」确认。
6. 在系统安装器点击 UPDATE，实际出现「App installed.」；系统包信息为 code 4，打开后设置显示 `1.0.3-smoke（4）`，服务器配置仍保留。这不是只验证下载或伪造版本响应。
7. 隔离站点给 WebView 设置会话 Cookie，原生版本查询没有携带该 Cookie；302 重定向被明确拒绝。将真实 APK 下载响应挂起后退出设置、切换服务器、再释放旧响应，新服务器检查保持正确，不出现旧源下载/错误回调覆盖。
8. 清空仅验收模拟器的应用数据重新安装正式 code 3，首次设置页不显示无服务器的更新入口。连接当前公网 HTTPS 服务器后，真实账本登录页和原生更新查询均成功；把地址输入框改为未保存的无效端口再检查，仍使用原服务器并显示 1.0.2 最新。

### 当前主机与公网发布

- 更新前创建了权限受限的 PostgreSQL 自定义格式备份，位于现有 `~/.local/state/household-bills/backups/`，文件名以 `before-android-updates-` 开头；保留旧后端二进制用于部署回退。
- 已构建替换本机后端并重启既有 `household-bills.service`。服务工作目录为仓库 `backend/`，默认发布目录正确，无需修改私有环境或外部监听范围。
- 本机 `http://127.0.0.1:9890/api/app-update/` 与现有公网 `https://bill.bykkyy.top:888/api/app-update/` 实测返回 1.0.2。公网 HTTPS 下载的 APK SHA-256 与本机签名产物一致；Android 原生客户端也实际查询了该公网入口。
- 416 修复后，对真实运行服务的越界 Range 请求再次确认 `416` 且保留 `no-store` / `nosniff`。
- 当前公网验收使用原有 `:888` 入口；不带端口的 443 入口证书域名不匹配，未绕过证书验证或修改该端口配置。

### 未运行与限制

- 当前环境没有 Docker，`docker compose config --quiet` 未能运行；没有进行镜像构建或容器运行验收。
- 未执行 Android 真机、API 26–34、多厂商安装器、所有旋转/进程终止时序或真实账号登录/业务写入；未运行前端构建、后端广泛回归或数据库集成套件。
- 生命周期与错误覆盖以以上实测场景为限，不宣称所有网络/TLS 故障都经过本次模拟器复现。HTTPS 校验沿用系统实现，没有忽略证书入口。

## 正式交付与迁移

- 正式 APK：`android/app/build/outputs/apk/release/app-release.apk`，版本 `1.0.2` / code 3，51,396 字节。
- APK SHA-256：`8f370be9513ac324c4022bbcfe46733b567c95abc37de17ecf583697b71da683`。
- 签名证书 SHA-256：`4aedbbd0ea296d721d1263120fb78765bd30fd1ee8ce47c2eaad07843c6f8658`。
- 正式发布目录同时保存上述 APK 与 `latest.json`；临时 code 4/Debug 包和烟测程序不作为交付物、不发布到正式目录。
- 1.0.0 / 1.0.1 没有更新入口，需要先手动覆盖安装本次 1.0.2；以后从设置检查更新。每次正式发布必须使用相同签名密钥、保持包名并递增 versionCode，否则拒绝安装；更新不删除服务器数据。
- 服务端第一次部署此功能需升级/重启后端；后续只发布 APK 即可。分机复制必须保持先 APK 后清单的顺序；反代必须转发精确带尾斜杠的 API 路径，不能把安装包请求改写成 SPA。
- 网页 UI 资源刷新与 APK 更新为两个入口。HTTP 只建议可信内网；公网继续要求有效 HTTPS。下载校验不能替代服务器正确部署、私钥保管或系统安装确认。
