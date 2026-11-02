# Android 构建与运行环境（NixOS）

## 背景与目标

在 NixOS 主机上补充 `android/` 的构建与运行环境。此前主机只有 Node.js、Go 与桌面工具，没有 JDK、Android SDK、`adb` 或模拟器，无法执行 README 中的 Android 构建命令，也无法把 APK 装到设备上验证。目标是提供可重复进入的环境入口，并用真实构建、安装、启动确认可用。

## 实际变更

- 新增仓库根 `shell.nix`，用 nixpkgs `androidenv.composeAndroidPackages` 生成固定组合：Platform 35、Build Tools 35.0.0、platform-tools、cmdline-tools、emulator 与 `system-images;android-35;google_apis;x86_64`；JDK 为 `jdk17`。
- `toolsVersion = null` 关闭不再需要的旧 `tools` 包及其 JDK 8 依赖；未引入 NDK、CMake、Android Studio 或签名配置。
- shell 导出 `ANDROID_HOME`、`ANDROID_SDK_ROOT`、`JAVA_HOME`，并追加 `GRADLE_OPTS` 中的 `-Dorg.gradle.project.android.aapt2FromMavenOverride=<SDK>/build-tools/35.0.0/aapt2`，使 AGP 使用 nixpkgs 已修补的 `aapt2`，避免下载无法在 NixOS 运行的 FHS 版本。原有的 `GRADLE_OPTS` 会保留。
- `shellHook` 只导出变量，不写 `local.properties`、不启动模拟器、不修改用户目录。
- 在用户目录创建 API 35 x86_64 的 AVD `household-bills`（pixel_6 硬件配置），不属于仓库内容。
- 根 README 的 Android 构建说明补充 NixOS 的 `nix-shell` 入口与 AVD 命令；未修改业务代码、Gradle 工程或签名流程。

## 验证结果

工具链：

- `nix-shell --run 'java -version && adb version && emulator -accel-check'`：OpenJDK `17.0.20.1`、ADB `1.0.41`（platform-tools 37.0.1）、`KVM (version 12) is installed and usable.`。
- `./gradlew --no-daemon :app:testReleaseUnitTest :app:assembleDebug`：`BUILD SUCCESSFUL in 2m 34s`，地址单元测试通过，生成 `android/app/build/outputs/apk/debug/app-debug.apk`（60,912 字节）。构建日志仅有 AGP 对 `aapt2FromMavenOverride` 的实验性提示和 `MainActivity` 使用弃用 API 的提示。

模拟器与运行：

- `emulator -avd household-bills -no-window ...` 冷启动约 36 秒，`sys.boot_completed=1`，`ro.build.version.sdk=35`、`ro.product.cpu.abi=x86_64`。
- `adb install -r` 成功；`am start -W -n com.household.bills/.MainActivity` 返回 `Status: ok`，`topResumedActivity` 为该 Activity，进程存活。
- 原生设置页显示「服务器地址」「保存并连接」；输入 `http://10.0.2.2:8000` 后地址写入应用私有 `shared_prefs/household_bills.xml`（`server_origin`），WebView 拉取前端构建产物与 `LoginView`，后端日志返回 200；截图确认显示账本登录页与齿轮设置入口。

端到端所用临时服务（仅为本次验证，未写入仓库、未提交任何凭据）：

- 临时 PostgreSQL 17 集群：`initdb -D /tmp/hb-pg -U hb --auth=trust`，监听 `127.0.0.1:55432`，库 `household_bills`。
- 后端：`CGO_ENABLED=0 go build -trimpath -o /tmp/household-bills ./cmd/server`，以 `COOKIE_SECURE=false`、`ADMIN_PASSWORD`（测试用随机值）和上述 `DATABASE_URL` 启动于 `127.0.0.1:8000`；启动时迁移与管理员初始化成功。
- 前端：`npm run build`（`vue-tsc --noEmit && vite build`）成功，供后端提供静态资源。

未执行：Release 签名构建与 `:app:publishRelease`（本机没有正式密钥，未重建签名材料）；真机与 API 26–34 运行；登录后的业务操作；Android 单元测试以外的自动化测试。

## 网络与兼容性说明

- 首次安装时 `dl.google.com` 的模拟器与系统镜像下载只有约 100–190 KB/s，`cache.nixos.org` 亦出现超时。实际做法是从腾讯镜像下载同版本 zip，校验 SHA-1 与 nixpkgs 固定输出哈希一致后用 `nix-store --add-fixed sha1` 预置，再执行 `nix-shell`。无需改动 nixpkgs 或项目配置。
- Gradle Wrapper 的 `networkTimeout=10000` 在慢网下会下载发行包超时；已把 `gradle-8.11.1-bin.zip` 预置到 `~/.gradle/wrapper/dists/gradle-8.11.1-bin/<hash>/`，SHA-256 与 `gradle-wrapper.properties` 的校验值一致，之后 wrapper 直接复用。
- `shell.nix` 默认以 `<nixpkgs>` 求值，依赖主机的 Nix 渠道或 flake registry；不同 nixpkgs 版本可能给出不同的 SDK/模拟器修订，但版本参数固定为上述组合。
- 需要可用的 `/dev/kvm` 才能以可接受速度运行 x86_64 模拟器；没有 KVM 时仍可构建 APK，但模拟器运行不实用。
- SDK 位于只读的 Nix store，`sdkmanager` 不适合在其中追加安装组件；需要其它平台或镜像时应在 `shell.nix` 的 `composeAndroidPackages` 中增加并重新进入。
- 该环境不包含签名密钥；`assembleRelease` 在未设置四个 `HOUSEHOLD_BILLS_*` 变量时仍只生成 unsigned APK，`publishRelease` 会拒绝执行，与既有文档一致。
