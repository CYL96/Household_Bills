# 家庭账本

中文家庭收支管理 Web 应用。管理员创建家庭成员账号，成员独立登录并共享同一个家庭账本；现金、银行卡、微信等资金账户分别记录余额。

## 技术栈

- 前端：Vue 3、TypeScript、Vite、Element Plus、ECharts。
- 后端：Go 1.26+、Gin、pgx，单个程序提供 API 与前端静态文件。
- 数据库：PostgreSQL 17+。不需要 Redis，不支持 SQLite。
- 认证：PostgreSQL 持久化 Session、HttpOnly Cookie、CSRF 校验；密码使用 bcrypt。

## 功能

- 固定管理员账号 `admin`，通过环境变量配置密码；登录与退出。
- 管理员创建普通用户；用户管理入口和接口仅管理员可访问。
- 家庭成员共享账本读写权限，可查看成员名单。
- 创建、修改资金账户和期初余额，查看各账户当前余额。
- 新增、修改、删除收入与支出；按月份、账户、类型和关键词查询。
- 月度收入、支出、结余、分类支出和每日收支图表。
- 桌面和手机响应式页面。

当前仅支持人民币，不包含账户间转账、自由分配管理员角色或成员移除。公开注册和邀请码加入已关闭，用户只能由管理员创建。

## 管理员与用户管理

- 首次启动必须设置 `ADMIN_PASSWORD`，程序会自动创建 `admin` 管理员及名为“家庭账本”的家庭。没有内置默认密码；密码至少 10 个字符、最多 72 个 UTF-8 字节。
- 后续启动可不设置 `ADMIN_PASSWORD`，保留原密码。提供不同的有效密码会更新管理员密码，并注销该管理员的已有登录；普通用户的登录不受影响。提供相同密码不会注销会话。
- 管理员登录后进入“用户管理”，填写用户名和初始密码创建普通用户。新用户加入管理员所属家庭，不能创建更多管理员。
- 普通用户看不到桌面或手机导航中的“用户管理”；直接访问 `/users` 会返回总览，直接请求用户管理 API 会收到 `403`。
- 更新后已有用户仍是普通用户，原家庭与账目保留，不会自动合并到管理员家庭。若已有普通账号占用 `admin` 用户名，初始化会报错，不会擅自提权或覆盖该账号。


## Docker Compose 运行

需要 Docker 和 Docker Compose。

```sh
cp .env.example .env
openssl rand -hex 32
```

将生成的十六进制密码填写到 `.env` 的 `POSTGRES_PASSWORD`，并填写 `ADMIN_PASSWORD` 作为首次启动的管理员密码。数据库密码不能为空；使用十六进制可避免连接 URL 的特殊字符转义问题。

本机 HTTP 体验时，将 `.env` 中 `COOKIE_SECURE` 设置为 `false`，然后执行：

```sh
docker compose up -d --build
```

访问 <http://localhost:8080>，使用 `admin` 和配置的 `ADMIN_PASSWORD` 登录，再由管理员创建成员账号。没有演示数据或内置默认密码。应用端口仅绑定本机，数据库端口不对宿主机开放；数据库保存在 `postgres_data` 持久卷。

正式部署应在前面配置 HTTPS 反向代理，并使用 `COOKIE_SECURE=true`。代理需要保留原始 `Host`，或通过 `ALLOWED_ORIGINS=https://bills.example.com` 明确指定允许的访问来源。不要将普通 HTTP 下的开发配置直接暴露到公网。

## 本地开发

需要 Go 1.26+、Node.js 22+、npm、可连接的 PostgreSQL。先创建数据库和相应用户，再设置实际连接信息。

后端终端：

```sh
cd backend
export DATABASE_URL='postgres://USER:PASSWORD@127.0.0.1:5432/household_bills?sslmode=disable'
export ADMIN_PASSWORD='请替换为至少10字符的强密码'
export COOKIE_SECURE=false
export LISTEN_ADDR=127.0.0.1:8000
go run ./cmd/server
```

连接字符串中的 `USER`、`PASSWORD` 应替换为本机数据库凭据；公网或跨主机数据库连接应配置 TLS，不能照搬 `sslmode=disable`。数据库迁移在后端启动时自动执行，带版本记录和迁移锁。

前端终端：

```sh
cd frontend
npm ci
npm run dev
```

打开 <http://127.0.0.1:5173>。开发服务器将 `/api` 代理到 `127.0.0.1:8000`，保留浏览器来源用于 CSRF 校验。不需要将登录凭据存入浏览器 localStorage。

## 二进制与局域网测试

`test/start.sh` 用于构建并启动程序，默认监听 `0.0.0.0:9890`，无需单独启动前端开发服务器。需要预先创建可连接的 PostgreSQL 数据库。

首次构建并启动（需要 Go 1.26+、Node.js 22+ 和 npm）：

```sh
ADMIN_PASSWORD='请替换为至少10字符的强密码' ./test/start.sh --build
```

脚本已配置本地 `127.0.0.1:5432/household_bills` 测试库的默认连接，无需手动设置 `DATABASE_URL`；设置该环境变量可覆盖默认值。脚本包含明文测试凭据，不应提交到公共仓库。构建时使用 `npm ci` 和 `npm run build` 生成 `frontend/dist/`，再以 `CGO_ENABLED=0` 构建 `backend/household-bills`，最后直接执行该二进制。

首次启动必须显式配置管理员密码；初始化成功后，后续启动可以省略 `ADMIN_PASSWORD`。需要修改密码时，设置新的 `ADMIN_PASSWORD` 并重启程序。不要将管理员密码写入代码仓库。

后续直接启动已有二进制（不需要 Go、Node.js 或 Docker）：

```sh
./test/start.sh
```

脚本通过自身位置解析资源路径，可从任意工作目录调用。运行时需要**二进制和外置前端资源目录**，前端未嵌入二进制；部署到其他机器时应同时复制 `backend/household-bills`、`frontend/dist/` 和 `test/start.sh`，保持相对目录结构，或指定绝对路径：

```sh
BINARY_PATH=/opt/bills/household-bills \
FRONTEND_DIST=/opt/bills/public \
LISTEN_ADDR=0.0.0.0:8080 \
./test/start.sh
```

未设置 `DATABASE_URL` 时使用脚本内的本地测试库连接；其他环境应显式设置 `DATABASE_URL`。`BINARY_PATH` 可更改构建输出和启动的二进制位置；`FRONTEND_DIST` 用于指定已有前端资源，`--build` 始终构建项目默认的 `frontend/dist/`。二进制须匹配目标机器的操作系统和 CPU 架构。

同一局域网中的手机或电脑访问 `http://服务器局域网IP:9890`；使用其他端口时相应修改 URL。需放行服务器防火墙对应的 TCP 端口，且路由器不能启用客户端隔离。`0.0.0.0` 是监听地址，不是其他设备使用的访问地址。

脚本默认 `COOKIE_SECURE=false`，适合**可信局域网的 HTTP 测试**；登录凭据和账单通过明文 HTTP 传输，不应暴露到公网。HTTPS 反向代理部署应设置 `COOKIE_SECURE=true`。`ALLOWED_ORIGINS` 默认留空，保留后端同源校验，局域网 IP 无需逐个加入白名单。

脚本不会读取 `.env`，不会启动或创建数据库；按 `Ctrl+C` 停止程序，使用 `./test/start.sh --help` 查看参数。

## 后端配置

Go 程序通过进程环境变量读取配置，不会自动加载 `.env`；根目录 `.env` 由 Docker Compose 读取。

| 变量 | 用途 |
| --- | --- |
| `DATABASE_URL` | 必填，PostgreSQL 连接字符串；`test/start.sh` 已提供本地测试库默认值 |
| `ADMIN_PASSWORD` | 首次启动必填，初始化 `admin`；已有管理员时可省略，提供新值可更新密码 |
| `LISTEN_ADDR` | 监听地址，默认 `:8000` |
| `COOKIE_SECURE` | 默认 `true`；本机或可信局域网 HTTP 测试设为 `false`，`test/start.sh` 默认 `false` |
| `FRONTEND_DIST` | 前端构建目录，默认 `../frontend/dist` |
| `ALLOWED_ORIGINS` | 可选，逗号分隔的完整来源地址；不填写则要求同 Host |
| `TEST_DATABASE_URL` | 仅集成测试使用的独立数据库，会清空其中业务表 |

Docker Compose 另使用 `POSTGRES_PASSWORD` 和 `PORT`（默认 `8080`）。

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

## 记账规则与权限

- 用户属于一个家庭；所有业务查询和写入均由后端校验家庭归属。
- 家庭中的所有成员都能修改共享账本；流水保留原始录入成员。
- 金额以整数“分”存储与计算，API 使用两位小数的金额字符串；不使用浮点数计算余额。
- 当前余额 = 期初余额 + 全部收入 − 全部支出；月份筛选不改变账户余额。
- 支持负的期初余额和负余额；流水金额必须为正数，最多两位小数。
- 月度结余 = 当月收入 − 当月支出；总余额包含全部月份及期初余额。
- 密码至少 10 个字符，最多 72 个 UTF-8 字节；Session 有效期为 7 天。
- 写接口必须同时携带 Cookie、`X-CSRFToken` 及允许的 `Origin`；匿名登录也检查 CSRF。公开注册接口已移除。
- 刷新受保护页面时先恢复登录状态；Session 失效后，受保护 API 返回 `401`，前端清除登录状态并转到登录页。有效登录下的 CSRF 校验失败返回 `403`，不会自动重试或退出登录。

## 备份

```sh
docker compose exec -T db pg_dump -U household_bills -d household_bills -Fc > household-bills.dump
```

备份应另存到独立设备或存储位置，并定期实际恢复验证。恢复应使用独立数据库，避免覆盖正在使用的账本。停止容器不会删除数据卷；`docker compose down -v` 会删除数据库数据，不应作为普通停止命令使用。
