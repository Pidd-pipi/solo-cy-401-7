# GigMatch · 自由职业者撮合平台

连接需求方与自由职业者的撮合平台：需求发布、报价竞标、合同签订与项目交付全流程管理。

## 快速启动（Docker Compose 一键部署）

```bash
cp .env.example .env
docker compose up -d --build
```

- 前端：http://localhost:28030
- 后端 API：http://localhost:29068
- API 文档：http://localhost:29068/docs
- 健康检查：http://localhost:29068/healthz
- 就绪检查：http://localhost:29068/readyz

测试账号（密码均为 demo123456）：
- `requester01`（需求方）
- `freelancer01` / `freelancer02`（自由职业者）
- `admin`（管理员）

## 项目主要功能

- 需求大厅：瀑布流列表 + 预算/技能/状态多维筛选
- 需求详情：完整信息 + 报价列表（需求方视角）+ 报价提交表单（自由职业者视角）
- 我的工作台：分角色展示已发布需求、已报价项目、进行中合同
- 合同详情：条款、阶段进度（分阶段付款进度条）、双方信息
- 争议案件：合同进行中/待验收时任一当事方可提交问题说明、诉求与证据；案件开启即冻结合同完成与阶段推进，双方可补充材料或撤回，撤回后恢复原流程；管理员可裁决全额归乙方、全额退回甲方或按比例结算（金额严格守恒）
- 个人资料：展示/编辑个人信息、技能标签、历史项目
- 横切：JWT 认证授权、操作日志、路由守卫、请求拦截器自动带 token

## 本地开发

### 后端（Go 1.22 + Gin + GORM）

```bash
cd backend
go mod tidy
go run ./cmd/server
```

构建检查：`go build ./...`；测试：`go test ./...`

### 前端（Vue 3 + TypeScript + Element Plus + Vite）

```bash
cd frontend
npm install
npm run dev
```

## 技术栈

| 层级 | 技术 |
| --- | --- |
| 后端 | Go 1.22 + Gin + GORM |
| 数据库 | MySQL 8.0 |
| 认证 | JWT（golang-jwt/v5）+ bcrypt |
| 前端 | Vue 3 + TypeScript + Element Plus + Vite + Pinia |
| 部署 | Docker Compose（Nginx 反代 + 多阶段构建） |

## 目录结构

```
├── backend/
│   ├── cmd/server/main.go
│   ├── database/init.sql       # 建库脚本（建表由 GORM AutoMigrate）
│   └── internal/
│       ├── config/  ├── model/  ├── repository/  ├── service/
│       ├── handler/ ├── router/ ├── middleware/  ├── dto/
│       ├── constants/ ├── util/ ├── logger/      └── docs/
├── frontend/                   # Vue 3 前端（按提示词生成）
│   └── src/
│       ├── api/      ├── stores/      ├── types/
│       ├── components/common/ ├── hooks/
│       ├── pages/    ├── router/      ├── utils/
│       └── constants/
├── docker-compose.yml
└── .env.example
```

## 环境变量

| 变量 | 说明 | 默认 |
| --- | --- | --- |
| COMPOSE_PROJECT_NAME | Compose 项目名 | gigmatch |
| FRONTEND_PORT / BACKEND_PORT / DB_PORT | 端口 | 28030 / 29068 / 33301 |
| DB_NAME / DB_USER / DB_PASSWORD / DB_ROOT_PASSWORD | 数据库配置 | gigmatch / gigmatch / gigmatch123 / root_pwd |
| JWT_SECRET | JWT 签名密钥，生产必须替换为 32 位以上随机值 | 见 .env.example |
| CORS_ALLOWED_ORIGINS | 允许跨域来源，逗号分隔；生产禁止 `*` | http://localhost:28030 |
| AUTH_RATE_LIMIT / API_RATE_LIMIT | 登录注册/业务接口限流（次/分钟/IP） | 10 / 120 |
| DB_MAX_OPEN_CONNS / DB_MAX_IDLE_CONNS | 数据库连接池 | 25 / 5 |
| DB_CONN_MAX_LIFETIME_MIN | 连接最大存活时间（分钟） | 5 |
| DB_CONNECT_RETRIES / DB_CONNECT_RETRY_INTERVAL_SEC | 启动重试次数/间隔 | 10 / 3 |

## Docker 部署说明

- 端口映射：前端 `${FRONTEND_PORT}:80`、后端 `${BACKEND_PORT}:8080`、MySQL `${DB_PORT}:3306`
- 前端 Nginx 将 `/api/` 反代到 `http://backend:8080/api/`
- 数据库命名卷 `db_data` 持久化；db → backend → frontend 健康依赖链
- 支持中文目录名部署（顶层 `name: gigmatch`）

## 争议案件规则

- **提交**：仅合同甲方/乙方可立案，且合同须处于 `in_progress` 或 `pending_review`；提交内容为问题说明、诉求与证据列表。同一合同同时只允许一个未结案件（数据库 `active_dispute_id` 唯一约束 + 事务条件更新双重保证，重复提交返回 409）。
- **冻结**：立案后合同 `activeDisputeId` 指向该案件，完成确认与后续阶段推进一律拒绝（返回"争议处理中，合同完成与阶段推进已暂停"）。
- **协商**：案件未结期间，双方均可补充材料；任一方可撤回。补充、撤回、裁决均在数据库事务内执行，并通过案件版本号（`version`）做乐观并发仲裁——客户端携带立案/刷新时观察到的版本提交，服务端执行 `UPDATE … WHERE status='open' AND version=?` 条件占用：并发开始的同类操作只有一个能占用成功，其余匹配 0 行，收到明确 409（"案件状态已被并发操作改变，请刷新后重试"）。MySQL 下的死锁牺牲者（1213）/锁超时（1205）/唯一键冲突（1062）同样映射为 409。撤回后解除冻结，合同状态与阶段保持原样，原流程恢复。
- **裁决**：仅管理员可裁决，方式为 `full_to_b`（全额归乙方，合同完成）、`full_refund_a`（全额退回甲方，合同终止）或 `proportional`（按比例，两比例之和必须等于 1）。按比例结算按"分"取整、乙方拿余数，保证 `甲方金额 + 乙方金额 = 合同总额` 分毫不差；不守恒的请求整体拒绝，案件保持未结、合同保持冻结。
- **终态一致**：凡合同收口为 `completed`（`full_to_b` 与 `proportional` 共用同一套终态规则），全部阶段同步置为 `done`，不会残留进行中/待处理里程碑；`full_refund_a` 收口为 `terminated`，阶段保持原样。案件结案后任何补充材料一律拒绝，材料、案件状态与合同结果始终一致。
- **一致性**：案件状态、合同状态/冻结标记、结算快照（`contracts.settlement`）在同一数据库事务内更新，操作记录（`dispute.open/supplement/withdraw/resolve`）在事务提交后写入；双方工作台与合同卡片实时显示未结争议计数与冻结标识。

## 枚举出现位置清单

| 枚举 | 后端定义 | 前端定义 | 前端消费 |
| --- | --- | --- | --- |
| RequirementStatus（draft/open/bidding/in_progress/pending_review/completed/cancelled） | `backend/internal/constants/requirement_status.go` | `frontend/src/types/enums.ts` | Requirements、RequirementDetail、Dashboard |
| BidStatus（pending/accepted/rejected/withdrawn） | `backend/internal/constants/bid_status.go` | `frontend/src/types/enums.ts` | RequirementDetail、Dashboard |
| ContractStatus（pending_signature/in_progress/pending_review/completed/terminated） | `backend/internal/constants/contract_status.go` | `frontend/src/types/enums.ts` | ContractDetail、Dashboard |
| DisputeStatus（open/withdrawn/resolved） | `backend/internal/constants/dispute_status.go` | `frontend/src/types/enums.ts` | ContractDetail、Dashboard、DisputePanel |
| DisputeVerdict（full_to_b/full_refund_a/proportional） | `backend/internal/constants/dispute_status.go` | `frontend/src/types/enums.ts` | ContractDetail、DisputePanel |
| UserRole（requester/freelancer/both/admin） | `backend/internal/constants/roles.go` | `frontend/src/types/enums.ts` | Layout、RequirementDetail、Dashboard |

## 主要 API 列表

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| POST | /api/v1/auth/register | 注册 |
| POST | /api/v1/auth/login | 登录 |
| GET | /api/v1/auth/me | 当前用户 |
| GET/POST | /api/v1/requirements | 需求大厅/发布 |
| GET/PUT | /api/v1/requirements/:id | 详情/编辑 |
| POST | /api/v1/requirements/:id/status | 状态流转 |
| POST | /api/v1/requirements/:id/accept-bid | 采纳报价并生成合同 |
| GET/POST | /api/v1/bids | 报价列表/提交 |
| POST | /api/v1/bids/:id/withdraw | 撤回报价 |
| GET | /api/v1/contracts | 我的合同 |
| GET | /api/v1/contracts/:id | 合同详情 |
| POST | /api/v1/contracts/:id/sign · /complete | 签署/完成 |
| GET/POST | /api/v1/contracts/:id/disputes | 合同案件历史/提交争议 |
| GET | /api/v1/disputes | 待裁决案件（管理员） |
| GET | /api/v1/disputes/:caseId | 案件详情（当事方/管理员） |
| POST | /api/v1/disputes/:caseId/supplement · /withdraw | 补充材料/撤回 |
| POST | /api/v1/disputes/:caseId/resolve | 管理员裁决（全额乙方/全额甲方/按比例） |
| GET | /api/v1/dashboard | 我的工作台（含未结争议） |
| GET/PATCH | /api/v1/users/:id | 个人资料 |
| GET | /api/v1/operation-logs | 操作日志 |
| GET | /healthz、/readyz | 健康检查 |

统一响应格式：`{ "code": 0, "message": "ok", "data": ... }`

## License

MIT
