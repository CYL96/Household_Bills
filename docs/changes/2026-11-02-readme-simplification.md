# README 精简为面向使用与部署

## 背景与目标

根 `README.md` 已累积大量实现细节与逐次 UI 说明（技术栈、记账规则内部字段、分类交互细则、本地开发、测试、二进制脚本、Android 打包与发布），对需要部署或只是想了解项目的人理解成本过高。目标是把 README 收敛为「项目介绍 + 部署 + 必要使用说明」，技术内容另存到开发文档，避免信息丢失。

## 实际变更

- 重写 `README.md`（由 285 行降至 106 行，减少约 63%），按非技术读者组织：
  - 项目简介与适用场景；「能做什么」用用户语言描述家庭共享、账户余额、记账、查账统计、身份安全与手机客户端。
  - Docker Compose 部署：复制 `.env.example`、生成数据库密码、首次管理员密码、HTTP 试用开关、启动与登录、数据卷说明。
  - 正式部署：HTTPS 反向代理要求（目标端口、`Host`/`X-Forwarded-Proto`、`ALLOWED_ORIGINS`、`3m` 请求体）、备份命令与恢复提醒、`down -v` 风险。
  - 只保留部署相关配置表（`POSTGRES_PASSWORD`、`ADMIN_PASSWORD`、`PORT`、`COOKIE_SECURE`、`ALLOWED_ORIGINS`、`ANDROID_RELEASE_HOST_DIR`）。
  - 本机 systemd 部署压缩为服务路径、监听地址、环境文件权限、常用命令与 Nginx 反代要点，保留原有域名与端口信息。
  - Android 客户端改为使用者视角：服务器地址填写、设置入口、刷新页面缓存、应用内更新与 HTTP 限制。
  - 「使用要点」保留用户可感知规则：管理员/成员关系、余额与转账的记账口径、归属标记、记账时间、共享分类冲突、记住登录。
- 新增 `docs/development.md`，承接从 README 移出的技术内容：技术栈、本地开发、测试与独立测试库要求、`test/start.sh` 二进制与局域网测试、完整后端环境变量表、Android 构建/签名/发布与更新接口约定。
- `AGENTS.md` 同步文档边界：README 面向非技术读者，开发/测试/构建/发布写入 `docs/development.md`；移除“README 转账说明过期”的旧提示（新文已包含转账与正确余额口径），并在重要文件清单登记 `docs/development.md`。
- 未修改任何代码、配置或部署文件；`docs/changes/` 历史记录保持原样。

## 验证结果

- 统计：`README.md` 由 285 行降至 106 行；`docs/development.md` 150 行，承接原 README 的开发、测试、打包与发布章节。
- 链接检查：脚本解析 `README.md` 与 `docs/development.md` 的全部相对链接，`docs/development.md`（3 处）、`docs/changes/README.md`、`../README.md` 均存在。
- 事实核对：部署步骤与 `compose.yaml`、`.env.example` 一致（`127.0.0.1:${PORT:-8080}`、`postgres_data` 卷、`COOKIE_SECURE` 默认 `true`、`ANDROID_RELEASE_HOST_DIR` 只读挂载）；systemd 段落沿用原有服务路径、`127.0.0.1:9890`、环境文件权限与允许来源；Android 最低版本、更新入口与签名说明与工程配置一致。
- 未执行：未运行 Docker Compose（本机未安装 Docker），未实际执行备份/恢复或 HTTPS 反向代理；本次仅文档改动，未运行前后端构建与测试。

## 兼容性与迁移

- 文档结构变化：README 不再包含开发、测试、打包和发布步骤，相关命令改由 `docs/development.md` 提供；内部文档和后续任务应引用新路径。
- 部署命令与配置项未变，现有部署无需调整；README 中的主机专属信息（systemd、域名、目录）仍是当前主机的实际情况。
