# 开发与构建

根目录 [README](../README.md) 面向使用与部署；本文件保留开发、测试、打包和发布的实现细节。

## 技术栈

- 前端：Vue 3、TypeScript、Vite、Element Plus、ECharts。
- 后端：Go 1.26+、Gin、pgx；单个程序同时提供 API 与前端静态文件。
- 数据库：PostgreSQL 17+。不需要 Redis，不支持 SQLite。
- 认证：PostgreSQL 持久化 Session、HttpOnly Cookie、CSRF 校验；密码使用 bcrypt。

## 本地开发

需要 Go 1.26+、Node.js 22+、npm 和可连接的 PostgreSQL。先创建数据库和相应用户，再设置实际连接信息。

后端：

```sh
cd backend
export DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/household_bills?sslmode=disable'
export ADMIN_PASSWORD='请替换为至少10字符的强密码'
export COOKIE_SECURE=false
export LISTEN_ADDR=127.0.0.1:8000
go run ./cmd/server
```

连接字符串中的 `USER`、`PASSWORD` 应替换为本机数据库凭据；公网或跨主机数据库连接应配置 TLS，不能照搬 `sslmode=disable`。数据库迁移在后端启动时自动执行，带版本记录和迁移锁。

前端：

```sh
cd frontend
npm ci
npm run dev
```

打开 <http://127.0.0.1:5173>。开发服务器将 `/api` 代理到 `127.0.0.1:8000`，保留浏览器来源用于 CSRF 校验。登录凭据不写入浏览器 localStorage。

## 测试

```sh
cd frontend
npm run build
cd ../backend
go test ./...
```

未设置 `TEST_DATABASE_URL` 时会跳过真实数据库集成测试。完整验证请先创建**独立且可清空的测试数据库**：

```sh
cd backend
TEST_DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/household_bills_test?sslmode=disable' go test ./...
```

不要将生产库或开发库作为测试库。集成测试会清空测试数据库的业务表；测试库不能与 `DATABASE_URL` 相同。

## 二进制与局域网测试

`test/start.sh` 用于构建并启动程序，默认监听 `0.0.0.0:9890`，无需单独启动前端开发服务器。需要预先创建可连接的 PostgreSQL 数据库。

首次构建并启动（需要 Go 1.26+、Node.js 22+ 和 npm）：

```sh
ADMIN_PASSWORD='请替换为至少10字符的强密码' ./test/start.sh --build
```

脚本已配置本地 `127.0.0.1:5432/household_bills` 测试库的默认连接，无需手动设置 `DATABASE_URL`；设置该环境变量可覆盖默认值。脚本包含明文测试凭据，不应提交到公共仓库。构建时使用 `npm ci` 和 `npm run build` 生成 `frontend/dist/`，再以 `CGO_ENABLED=0` 构建 `backend/household-bills`，最后直接执行该二进制。

后续直接启动已有二进制（不需要 Go、Node.js 或 Docker）：

```sh
./test/start.sh
```

不带 `--build` 只会运行已有二进制，不会编译修改后的后端源码。后端接口有变更时，应先停止旧服务，再保留原有配置执行 `./test/start.sh --build`。仅更新 `frontend/dist/` 而未重建并重启后端，会导致新页面调用新接口时出现“接口不存在”。

脚本通过自身位置解析资源路径，可从任意工作目录调用。运行时需要**二进制和外置前端资源目录**，前端未嵌入二进制；部署到其他机器时应同时复制 `backend/household-bills`、`frontend/dist/` 和 `test/start.sh`，保持相对目录结构，或指定绝对路径：

```sh
BINARY_PATH=/opt/bills/household-bills \
FRONTEND_DIST=/opt/bills/public \
LISTEN_ADDR=0.0.0.0:8080 \
./test/start.sh
```

同一局域网中的手机或电脑访问 `http://服务器局域网IP:9890`；需放行防火墙对应 TCP 端口，且路由器不能启用客户端隔离。`0.0.0.0` 是监听地址，不是其他设备使用的访问地址。

脚本默认 `COOKIE_SECURE=false`，适合**可信局域网的 HTTP 测试**；HTTPS 反向代理部署应设置 `COOKIE_SECURE=true`。脚本不会读取 `.env`，不会启动或创建数据库；按 `Ctrl+C` 停止程序，使用 `./test/start.sh --help` 查看参数。

## 后端配置

Go 程序通过进程环境变量读取配置，不会自动加载 `.env`；根目录 `.env` 由 Docker Compose 读取。

| 变量 | 用途 |
| --- | --- |
| `DATABASE_URL` | 必填，PostgreSQL 连接字符串；`test/start.sh` 已提供本地测试库默认值 |
| `ADMIN_PASSWORD` | 首次启动必填，初始化 `admin`；已有管理员时可省略，提供新值可更新密码 |
| `LISTEN_ADDR` | 监听地址，默认 `:8000` |
| `COOKIE_SECURE` | 默认 `true`；本机或可信局域网 HTTP 测试设为 `false` |
| `FRONTEND_DIST` | 前端构建目录，默认 `../frontend/dist` |
| `ANDROID_RELEASE_DIR` | APK 发布目录，默认 `../android/release`，相对进程工作目录；Docker 镜像内为 `/app/android-release` |
| `ALLOWED_ORIGINS` | 可选，逗号分隔的完整来源地址；不填写则要求同 Host |
| `TEST_DATABASE_URL` | 仅集成测试使用的独立数据库，会清空其中业务表 |

## 网页自动更新与部署

生产构建为 HTML 的 `hb-build-id` meta 与 JavaScript 注入同一个构建标识；开发服务器不启用自动更新。页面启动、重新可见、恢复联网、App 回前台以及前台每两分钟检查站点根入口，使用 `cache: no-store`，失败不刷新、不弹错误。

发现新构建后，等待编辑弹窗、分类未保存草稿、登录输入、确认框和写请求结束，再重新加载当前完整 URL（保留 query/hash）。不清 Cookie、localStorage 或 WebView 资源缓存；仅在内存中的筛选和滚动位置不保证保留。原生客户端 1.0.6 起同时通知网页前后台与原生设置遮挡状态，旧客户端仍可通过网页可见性和前台周期检查更新。

部署必须保持入口和资源一致：

- `/`、SPA 路由和直接访问的 `/index.html` 均需重验证，不得被反向代理设为长期缓存；`/assets/` 下带内容哈希的资源保持长期缓存。
- 在临时目录构建，例如从 `frontend/` 执行 `npm run build -- --outDir /tmp/household-bills-web-build`。**不要对正在被服务进程读取的 `frontend/dist` 原地构建**，Vite 的 `emptyOutDir` 会先清空目录。
- 先复制新构建的资源，保留旧哈希资源供已打开的页面加载分包；最后在同一文件系统内原子替换 `index.html`。HTML 中的 meta 和脚本必须来自同一次构建，无需额外版本 JSON。
- 首次部署本机制后，已打开的旧网页没有检测代码，需手动刷新一次。此后网页升级与 APK 更新分开；HTML 缓存策略修复需更新后端二进制。

自动刷新会话只在 sessionStorage 记录已尝试的目标构建，防止代理返回旧入口导致反复刷新；若无法写入该记录，保留当前页面，由用户手动重新加载。登录仍遵循原有会话有效期。

## Android 构建、签名与发布

`android/` 是独立的原生 Java 工程（包名 `com.household.bills`），Gradle Wrapper 8.11.1、AGP 8.9.2、JDK 17、compile/target SDK 35、minSdk 26，无第三方运行时依赖。

开发构建：

```sh
cd android
./gradlew :app:testReleaseUnitTest :app:assembleDebug
```

调试 APK 位于 `android/app/build/outputs/apk/debug/app-debug.apk`。

NixOS 主机可在仓库根执行 `nix-shell` 进入已配置好 JDK 17、Android SDK、`adb` 和 API 35 x86_64 模拟器镜像的环境；该 shell 还会让 Gradle 使用 SDK 内的 `aapt2`。首次运行可创建虚拟设备并启动（需要可用的 `/dev/kvm`）：

```sh
avdmanager create avd -n household-bills -k "system-images;android-35;google_apis;x86_64" --force
emulator -avd household-bills
```

正式构建需设置四个签名环境变量：`HOUSEHOLD_BILLS_KEYSTORE`（私钥库绝对路径）、`HOUSEHOLD_BILLS_KEYSTORE_PASSWORD`、`HOUSEHOLD_BILLS_KEY_ALIAS`、`HOUSEHOLD_BILLS_KEY_PASSWORD`，然后执行 `./gradlew :app:assembleRelease`；可安装的签名 APK 位于 `android/app/build/outputs/apk/release/app-release.apk`。未配置签名时只生成不能直接安装的 unsigned APK。私钥与密码不得提交到仓库，后续覆盖升级必须沿用同一签名并递增 `versionCode`。

### 发布 App 更新

服务器先部署包含应用更新接口的新版后端。构建机配置上述四个正式签名环境变量，递增 `android/app/build.gradle` 中的 `versionCode` 和 `versionName`，然后执行：

```sh
cd android
./gradlew :app:publishRelease
```

发布任务会构建签名 Release APK，将带版本号和 SHA-256 的 APK 写入 `android/release/`，再原子更新 `latest.json`；缺少正式签名配置会拒绝发布。不得把私钥或密码放进发布目录。`ANDROID_RELEASE_DIR` 可指定其它输出目录，相对路径以 `android/` 为基准。

后端通过 `ANDROID_RELEASE_DIR` 读取同一发布目录，默认 `../android/release`，相对路径以 **Go 进程工作目录** 为基准；从 `backend/` 启动时与上面的默认输出一致。分机部署时先复制 APK，再替换 `latest.json`，或整体原子切换已准备好的发布目录。目录和文件需对服务用户可读，发布目录不放入 `FRONTEND_DIST`。

- `GET /api/app-update/`：公开版本查询；未发布安装包时明确返回 `available:false`，不需要账本登录。
- `GET/HEAD /api/app-update/download/<sha256>/`：下载当前清单指向的安装包；支持 Range，不提供任意文件下载，也不会回落 SPA。
- 后端每次查询读取最新清单，后续发布 APK 不需要重启服务。网页 UI 使用上面的自动更新机制，「刷新页面缓存」仅作旧网页首次升级和异常兜底，与 APK 更新分开。

Docker Compose 将 `${ANDROID_RELEASE_HOST_DIR:-./android/release}` 只读挂载到 `/app/android-release`；镜像内 `ANDROID_RELEASE_DIR` 已固定指向该目录。自定义宿主目录时，建议给 Gradle 的 `ANDROID_RELEASE_DIR` 和 Compose 的 `ANDROID_RELEASE_HOST_DIR` 使用同一个宿主绝对路径。Compose 的 `.env` 不会自动成为宿主 Gradle 的进程环境；需要在构建终端单独设置。

覆盖升级必须保持包名和签名密钥不变，且新 `versionCode` 高于手机已安装版本。客户端不跟随更新重定向，不发送网页登录 Cookie，不跳过证书校验；反向代理需允许上述两个精确 API 路径。

## 变更记录

任务与需求的实现过程、验证结果和兼容性说明存放在 [`changes/`](changes/README.md)。
