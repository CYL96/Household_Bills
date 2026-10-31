# systemd 应用服务部署

## 目标与范围

将现有应用作为持续运行的服务部署在当前主机；Nginx、证书和 DNS 由部署者另行配置，本次没有修改。

## 实际配置

- 使用 `nixos` 的 systemd 用户服务 `household-bills.service`，已启用开机启动；主机的用户 linger 已开启。
- 监听 `127.0.0.1:9890`，反代目标为 `http://127.0.0.1:9890`，不直接向公网开放应用监听。
- 服务文件位于 `~/.config/systemd/user/household-bills.service`，环境文件位于 `~/.config/household-bills/environment`，后者权限为 `0600`。
- 执行程序为 `/home/nixos/github/Household_Bills/backend/household-bills`，静态文件目录为 `/home/nixos/github/Household_Bills/frontend/dist`。更新代码后需重新构建并重启服务。
- 保持 `COOKIE_SECURE=true`，允许 HTTPS 来源 `https://bill.bykkyy.top`、`https://bill.bykkyy.top:888`。公网入口变化时应同步调整允许来源。
- 保留原有 `household_bills` 数据库、管理员登录密码和账目。数据库连接密码已随机化，仅保存在私有环境文件；旧开发连接配置和原有主机秘密副本需要另行同步，不应继续使用旧测试密码。
- 迁移前使用 `pg_dump --format=custom` 备份至 `~/.local/state/household-bills/backups/`，文件权限为 `0600`；应用启动已将数据库迁移至版本 10。
- 第 10 号迁移使用新版支出分类设置替换原分类设置，不重写历史账目。

## 实际验证

- 后端：通过 Nix 提供的 Go 执行生产构建成功。
- 前端：`npm ci && npm run build` 成功；Vite 提示部分产物超过 500 kB，不影响构建完成。
- 服务启用、启动和主动重启成功；重启后状态为 `enabled`、`active`。
- 首页、当前构建的入口 JS、`/transactions` 路由均返回 HTTP 200。
- `/api/auth/me/` 返回 HTTP 200；匿名访问 `/api/accounts/` 返回 HTTP 401；未知 API 返回 JSON HTTP 404，未回退至 HTML。
- 使用有效 CSRF cookie/header 从两个已允许的 HTTPS 来源提交空登录表单，均返回预期的 HTTP 400 登录校验错误；不受信任来源返回 HTTP 403。
- 迁移后仍有 3 个账户、2 笔交易、1 个管理员，与迁移前账目数量一致。未使用真实用户密码进行完整登录验证。
- `pg_restore --list` 可读取迁移前备份，目录包含账户、交易和用户表。
- 未运行广泛回归测试；未进行浏览器外观测试；未验证公网域名或 Nginx 入口。

管理命令和反代要求见根目录 README 的“本机 systemd 服务”一节。
