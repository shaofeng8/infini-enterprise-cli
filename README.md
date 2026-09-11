# infini-cli

面向 ToB / 私有化部署的 InfiniSynapse 命令行工具。目标是让 CLI 能执行 Infini 的所有操作，重点覆盖看板的创建与编辑。

与 [`infinisynapse-cli`](../infinisynapse-cli)（`agent_infini`，面向 ToC 与 AI Agent 场景）并行维护，两者不共享代码包，各自独立发版。

完整功能清单与实施进度见 [PLAN.md](./PLAN.md)。

## 当前状态

- **P0 地基** ✅ 配置与多 profile、认证、HTTP/SSE 客户端、退出码语义、`api` 逃生舱、`events` 事件流
- **P1 看板 REST 轨** ✅ 列表、查看、导出/导入、版本回滚、布局、查询、刷新、筛选、上下文附件
- **P1 看板 Agent 轨** 待做（`dash new` / `dash edit`，即创建与编辑）
- 任务、数据源、知识库、项目等其余模块待做，期间可用 `infini-cli api` 直调

## 环境要求

- Go 1.26+
- Make（可选，也可直接用 `go build`）

## 构建

```bash
make build          # 产物在 build/infini-cli
make cross          # 交叉编译 linux/darwin/windows × amd64/arm64
make test           # 单元测试
make lint           # go vet
```

## 快速开始

```bash
# 1. 指向目标部署
infini-cli config set server https://infini.example.com

# 2. 登录（auth/proxy 地址会自动从后端发现）
infini-cli auth login --username alice@example.com

# 3. 自检：配置、连通性、凭证、授权状态
infini-cli config doctor --table

# 4. 看板
infini-cli dash ls --table

# 5. 任意接口直调（业务子命令尚未实现时的通用入口）
infini-cli api GET /api/ai/dashboards
```

## 看板

看板工作分两轨：**运维**（列表、查询、刷新、版本、布局）走 REST，已经实现；**创作**（把业务需求变成一块看板）走 Agent，因为 spec 背后是一整张 Infini-SQL DAG 和筛选契约，由服务端校验和水合。

```bash
infini-cli dash ls --table
infini-cli dash show <id> --table
infini-cli dash filter ls <id> --table          # 有哪些筛选，怎么传
infini-cli dash query <id> --filter period=last_30d --filter region=east --table
infini-cli dash refresh <id> --wait --force-refresh
infini-cli dash revisions <id> --table
infini-cli dash rollback <id> --revision 3
```

### 筛选值

筛选值的 JSON 类型取自看板自己的 spec，所以不用关心该传字符串还是数字：

```bash
--filter region=east                    # 文本或单选枚举
--filter tags=a,b                       # 多选枚举，也可重复传 --filter tags=a --filter tags=b
--filter min_pv=100                      # 数字
--filter period=2026-01-01..2026-01-31   # 显式日期区间
--filter period=last_30d                 # 日期区间预设
--filter-values @filters.json            # 完整报文，优先级高于 --filter
```

传错筛选名会在本地就报错并列出该看板的合法名字，不用等服务端拒绝。

### spec 往返

```bash
infini-cli dash export <id> -o board.json
# 编辑 board.json
infini-cli dash apply <id> board.json --change-brief "新增转化率卡片"
```

`export` 会把 `specHash` 写进文件，`apply` 先比对再写，看板被别人改过就中止。这道检查在客户端：REST 接口本身没有乐观锁，所以它挡的是「两个人同时改一块看板」的常见窗口，不是严格的竞态保护。`--force` 可跳过。

`dash import` 与 `dash apply` 都同时接受裸 spec 和导出的 bundle，所以手写的 spec 或从别的部署拷来的 spec 一样能用。

## 配置

配置文件默认在 `~/.infini-cli/config.yaml`，按 **profile** 组织，一个二进制可以在多个部署间切换：

```yaml
current-profile: prod
profiles:
  prod:
    server: https://infini.example.com
    console: https://api.example.com/api
    token: <auth login 写入的 JWT>
  staging:
    server: https://staging.example.com
```

取值优先级：**命令行 flag > 环境变量 > 活动 profile > 内置默认值**。

`INFINI_CONFIG` 可整体改写配置文件路径（CI 里用它做隔离）。

### Profile 操作

```bash
infini-cli config profile ls --table
infini-cli config profile add staging --with-server https://staging.example.com
infini-cli config profile use staging
infini-cli --profile staging api GET /api/ai/dashboards   # 单次切换，不改默认
```

`profile add` 只创建，不切换；切换一律用 `profile use`。

### 支持的配置项

| Key | 说明 |
|---|---|
| `server` | Infini 应用后端地址，所有业务接口都在其 `/api` 下 |
| `console` | auth/proxy 服务地址，负责签发 JWT 与用户/模型数据 |
| `api-key` | API Key 凭证 |
| `token` | `auth login` 写入的 JWT |
| `token-expires-at` | JWT 过期时间（只读展示） |
| `tenant-code` | 多租户部署的租户码，登录时使用 |
| `user-id` / `username` | 当前用户，登录后自动写入 |
| `prefer-language` | 请求头 `x-lang`，取值 `en` `zh_CN` `ar` `ja` `ko` `ru` |
| `default-output` | 默认输出格式，`json` 或 `table` |

## 认证说明

Infini 自己不签发凭证：应用后端只通过 `GET /api/auth/getAuthingPath` 告诉客户端 auth/proxy 服务在哪，JWT 由该 proxy 签发。`auth login` 一次完成「发现 proxy → 登录 → 校验 → 落盘」。

密码只支持交互式输入或 `--password-stdin`，不提供 `--password` flag —— 那会留在 shell 历史和进程列表里。

```bash
infini-cli auth login --username alice@example.com
echo "$PASSWORD" | infini-cli auth login --username alice --password-stdin  # CI
infini-cli auth whoami
infini-cli auth status     # 只看本地状态，不发请求
infini-cli auth logout
```

同时存在 JWT 和 API Key 时，JWT 优先；显式传入的 `--api-key` 优先于两者。

## 输出与退出码

默认输出 JSON 信封，可直接 `jq`：

```json
{ "success": true, "data": {}, "message": "" }
```

列表类命令加 `--table` 转人类可读表格。进度与诊断信息一律写 stderr，不污染 stdout。

| 退出码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 业务错误 |
| 2 | 参数错误、缺少确认 |
| 3 | 鉴权失败（含 token 过期） |
| 4 | 网络或服务不可达 |
| 5 | License 限额拦截 |

服务端错误码会被映射成可读提示，例如 1101/1105 提示重新登录，3410–3414 提示查看授权限额。

## 事件流

Agent 命令通道是异步的：`POST /api/ai/message` 只入队并立即返回，执行结果只通过 SSE 推送。`events` 是这条流的原始视图：

```bash
infini-cli events                                  # 全部事件
infini-cli events --task <taskId>                  # 只看某个任务
infini-cli events --event message.add --limit 20   # 过滤类型并自动停止
```

连接断开会自动退避重连；鉴权失败不重试。

## 危险操作

`rm` / `rollback` / `drain` / `archive` 一类操作默认需要二次确认。非交互环境（管道、CI）必须显式传 `--yes`，否则以退出码 2 终止。

## 调试

```bash
infini-cli --verbose api GET /api/ai/dashboards   # 打印请求摘要
infini-cli --trace   api GET /api/ai/dashboards   # 打印完整请求与响应
```

`--trace` 会自动脱敏 `password`、`token`、`api-key` 等字段。

## 项目结构

```
infini-enterprise-cli/
├── main.go                      # 入口，把 Execute() 的返回值作为退出码
├── cmd/
│   ├── root.go                  # 根命令、全局 flag、退出码收口、确认门
│   ├── auth.go                  # 登录与凭证
│   ├── config.go                # 配置、profile、doctor 自检
│   ├── dash*.go                 # 看板：CRUD、spec 往返、版本布局、查询、刷新
│   ├── api.go                   # 任意接口直调逃生舱
│   ├── events.go                # SSE 事件流
│   ├── helpers.go               # 参数解析、JSON 载荷读取
│   ├── term.go                  # TTY 检测与无回显输入
│   └── version.go
└── internal/
    ├── auth/                    # proxy 登录链（md5 口令、JWT、profile）
    ├── cliexit/                 # 退出码与修复提示
    ├── client/                  # HTTP 封装、响应信封解包、错误分类、SSE
    ├── config/                  # 多 profile 配置与优先级解析
    ├── dashboard/               # 看板 REST 封装、spec 模型、filter 类型转换
    └── output/                  # JSON / 表格输出
```
