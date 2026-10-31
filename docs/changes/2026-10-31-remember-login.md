# 记住登录

## 背景与目标

用户希望避免反复输入账号和密码。采用可选的持久登录，不将密码、密码哈希或会话令牌写入浏览器 localStorage/sessionStorage，也不实现明文密码回填。

## 实际变更

- 登录页新增“记住登录（30 天）”复选框，默认不勾选，提示“仅在个人设备上勾选”；登录请求期间禁用该选项，保留用户名和密码的浏览器自动填充属性。
- `POST /api/auth/login/` 请求新增布尔字段 `remember`：
  - `true`：服务端会话有效期 30 天，登录 Cookie 带对应的 `Expires` 和 `Max-Age`，重新打开浏览器时通过现有 `/api/auth/me/` 恢复登录。
  - `false` 或省略：登录 Cookie 不带持久化属性，服务端会话最长有效 12 小时。
- 登录 Cookie 继续使用 `HttpOnly`、`SameSite=Lax`，`Secure` 仍由 `COOKIE_SECURE` 控制。CSRF Cookie 保持原有 7 天有效期；缺失时沿用现有恢复逻辑。
- 退出登录继续撤销当前服务端会话，修改密码继续撤销该用户全部会话。保留既有会话令牌哈希存储与服务端到期校验。
- 新增真实 PostgreSQL 集成用例，覆盖三种请求方式的 Cookie/数据库有效期，以及持久会话恢复和登出后的令牌重放拒绝。更新根 README 中的稳定使用说明。

## 已执行的验证

- 基线实测：旧实现收到 `remember: true` 后仍下发 7 天登录 Cookie，`Max-Age=604800`。
- `frontend/` 执行 `npm run build`：TypeScript 检查和 Vite 构建通过；构建提示部分产物超过 500 kB，未为本需求调整打包策略。
- 在独立、可清空的 PostgreSQL 17 测试数据库中执行并通过：
  - `TestLoginRememberSessionLifetime`：勾选、未勾选、省略字段；验证 HttpOnly、Cookie 持久属性及对应的 30 天/12 小时数据库期限。
  - `TestRememberedSessionRestoresAfterBrowserRestartAndLogoutRevokes`：持久令牌在新客户端恢复身份，退出后重放无法恢复身份。
  - `TestAuthSessionAndRemovedRegistration`：复用原有 CSRF、服务端过期拒绝、退出登录及重放校验。
  - `TestUserProfilePasswordRotation`：复用原有改密后会话失效校验。
- 使用实际 Go 服务和 Chromium 页面冒烟：
  - 默认未勾选；键盘空格可切换复选框。
  - 390 × 844 移动视口截图检查布局，并确认文档宽度与视口均为 390，无横向溢出。
  - 勾选后登录成功，下发 30 天 HttpOnly Cookie；localStorage 和 sessionStorage 均为空。
  - 完全关闭 Chromium 进程，使用相同浏览器配置目录重新启动，直接进入总览，无需重新输入凭据。
  - 从界面主动退出，返回登录页且登录 Cookie 被清除；未勾选重新登录，Cookie 的 `expires=-1`，实际 `/api/auth/me/` 返回已登录身份。
- 视觉限制：验证环境无中文字体，截图中文显示缺字方框；中文文案通过页面无障碍树核对，未声称验证中文字体渲染。
- 未运行全量后端回归、Android 构建或 Android 真机验证；Android 端未改动，沿用现有 Cookie 持久化机制。

## 兼容与安全说明

- 不需要数据库迁移，沿用 `sessions.expires_at`。升级前已创建的会话保持原有到期时间。
- 新后端收到未携带 `remember` 的旧登录请求时，采用临时会话和 12 小时上限；前后端应一起部署，旧后端不会提供新的 30 天期限。
- 30 天为固定上限，不在每次访问时自动续期。主动退出、改密或会话被撤销后，需要重新登录。
- 浏览器会话恢复或 Android WebView 可能保留临时 Cookie，不能把关闭窗口或应用当作可靠退出；公共设备应主动退出登录。
