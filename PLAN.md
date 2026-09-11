# infini-enterprise-cli 执行清单

面向 ToB / 私有化部署的 InfiniSynapse 命令行工具。目标：**CLI 可执行 Infini 的所有操作**，重点覆盖看板的创建与编辑。

与现有 `infinisynapse-cli`（`agent_infini`，ToC + AI Agent 场景，仅覆盖 task/db/rag 约 5% 接口面）并行维护，不替换、不合仓。

---

## 1. 目标与非目标

### 目标

- 覆盖 Infini 后端全部 24 个 controller、约 200 个 REST 端点
- 覆盖 Agent 命令通道 `POST /api/ai/message` 的全部 30 种命令类型
- 看板全生命周期：列表、详情、创建、编辑、查询、刷新、版本、布局、删除
- 支持私有化部署：自定义 server / console、多 profile、多租户、License 感知
- 人机双用：交互式 TTY 输出 + `--json` 管道输出 + 非交互脚本模式（CI 可用）

### 非目标

- 不在客户端重新实现 `dashboard-builder` 的 spec 生成逻辑（见 §3.2）
- 不做 Web UI 的替代品（图表渲染、拖拽布局仅做数据与坐标层）
- 不接管 `agent_infini` 的 WinClaw 工具市场分发通道

---

## 2. 技术选型

| 项 | 决定 | 理由 |
|---|---|---|
| 二进制名 | `infini` | 与 ToC 的 `agent_infini` 明确区分 |
| Module path | `github.com/chaozwn/infini-enterprise-cli` | 与现有 CLI 同 owner |
| 语言 | Go 1.26+ | 复用 `infinisynapse-cli` 已验证的 HTTP/SSE/配置实现，单文件分发 |
| 命令框架 | `spf13/cobra` | 与现有 CLI 一致，子命令层级深时可控 |
| 表格输出 | `olekukonko/tablewriter` | 同上 |
| 配置格式 | YAML（`go.yaml.in/yaml/v3`） | 与 `~/.agent_infini/config.txt` 结构兼容 |

### 从 `infinisynapse-cli` 移植（不重写）

- `internal/client/client.go` — HTTP 封装、Bearer、`x-lang`、`APIResponse.code` 解包、1101/1105 token 失效识别
- `internal/client/sse.go` — SSE 流式读取
- `internal/output/output.go` — JSON / Table 双输出
- `internal/config/config.go` — 凭证加载链（需扩展为多 profile）

---

## 3. 三个必须先定下来的核心机制

这三点决定了命令层怎么写，务必在 P0 完成前定稿。

### 3.1 Agent 命令通道是异步队列，不是同步 RPC

`POST /api/ai/message` **只把命令写入 `ai_task_command` 并入队，立刻返回**，真正执行由持有任务 lease 的 Worker 完成，结果只通过 SSE 推送。

因此任何 Agent 类命令（`newTask`、`askResponse`、回滚等）在 CLI 侧都必须是这套流程：

1. 生成 `clientOperationId`（HTTP 重试必须复用同一个值，保证幂等）
2. `POST /api/ai/message`，带 `protocolVersion: 2`，拿到 `taskId` + `commandId`
3. 订阅 `GET /api/ai/events?connId=<uuid>`，跟踪 `command.state`（queued → running → done）
4. 消费 `message.add` / `message.update` / `message.partial` 做流式输出
5. 收到 `state.ready` 后**必须回头全量拉取**（`GET /api/ai_task/getTaskData` 等），以数据库状态为准，不能只依赖增量事件

例外（同步返回，不入队）：`autoApprovalSettings`、无 `taskId` 的 `updateChatMode` / `togglePlanActMode`、`killActiveJobs`。

- [ ] 实现 `internal/agent/command.go`：命令入队 + `clientOperationId` 幂等 + `commandId` 状态机
- [ ] 实现 `internal/agent/events.go`：SSE 多路复用，按 `taskId` 分发，heartbeat 保活与断线重连
- [ ] 实现 `state.ready` 后的全量对账（reconcile）逻辑
- [ ] `clearTask` 不带 `taskId` 时服务端会展开成 per-task 命令，CLI 需处理返回的 `commandIds` 数组

### 3.2 看板是双轨制：REST 做运维，Agent 做创作

后端注释写得很明确：

```
@ApiOperation({ summary: '创建看板（导入/兜底场景，常规创建走 agent）' })
@Post()
```

Web 前端**没有** `createDashboard()`；点「对话创建」发的是一条 `newTask`，提示词就是 `messages.dashboard.newViaChatPrompt`（引导 → 确认蓝图 → 再查数创建）。真正干活的是后端 `dashboard-builder` skill，通过 `dashboard_read` / `dashboard_submit` 两个工具落地。

CLI 必须照抄这个分工：

| 轨道 | 用途 | 手段 |
|---|---|---|
| 确定性轨（REST） | 列表、详情、导出/导入 spec、删卡片、版本、回滚、布局、查询、刷新、筛选项 | 直接调 `/api/ai/dashboards/*` |
| 创作轨（Agent） | 新建看板、按需求改看板、优化布局 | 转 `newTask` / `askResponse`，复用 Web 的引导提示词 |

**不要**在 CLI 里自己拼 DashboardSpec 再 `POST /api/ai/dashboards`。那会跳过 spec 的 zod 校验、Infini-SQL Notebook DAG 水合、`${dash_filter_*}` Filter Contract、datasource id 推断，只适合"导入已有 spec"这一种场景。

编辑看板走 REST 时注意乐观锁：`PUT` 需要先 `GET` 拿 `specHash`，Agent 轨则是 `expected_spec_hash`。

- [ ] 明确 `dash new` / `dash edit` 走 Agent 轨，并在 `--help` 里写清
- [ ] `dash import` 走 REST `POST`，标注为"导入/兜底"
- [ ] 实现 spec 的本地文件往返：`dash export <id> -o spec.json` → 编辑 → `dash import`

### 3.3 认证与租户：api-key 不够用

现有 `agent_infini` 只支持个人 API Key 当 Bearer。ToB 还需要：

- **JWT 登录**：走 `infini-proxy` 的 `POST /auth/loginByUsername`（口令为 `md5(trim(password))`，与 Web 端一致）或 `POST /auth/login`，再用 `GET /user/getJwtProfile` 校验
- **proxy 地址发现**：`GET /api/auth/getAuthingPath`、品牌码 `GET /api/auth/getBrand`
- **租户上下文**：`tenantId` 由服务端从认证结果注入，客户端传的同名字段会被覆盖 —— CLI 不要自己伪造
- **License 限额**：`GET /api/license/limits` 需登录态；`newTask` 会被 `LicenseGuardService.assertCanCreateTask` 拦截，CLI 要能把限额错误翻译成可读提示
- **多 profile**：一套二进制要能在多个私有化环境 / 多个账号间切换

- [ ] 配置结构从单 `global` 扩展为 `profiles.<name>`，保留 `global` 兼容读取
- [ ] `--profile` 全局 flag + `INFINI_PROFILE` 环境变量
- [ ] Token 过期自动刷新（1101 / 1105 触发重登，而非直接报错退出）

---

## 4. 命令清单（按后端模块全量映射）

全局前缀 `/api`。下表是命令设计与端点的对应关系，勾选即代表该组已实现并自测通过。

### 4.1 `infini auth` / `infini config` — 认证与配置

- [x] `auth login [--username] [--password-stdin]` → proxy `/auth/loginByUsername`，自动发现 console 并落盘
- [x] `auth whoami` → `/user/getJwtProfile`
- [x] `auth logout`（`GET /auth/logout` + 清本地凭证）、`auth status`（纯本地）
- [x] `auth mode` → `/api/auth/getAuthingPath`、`/api/auth/getBrand`
- [x] `config ls` / `get` / `set` / `unset` / `path`
- [x] `config profile ls` / `use` / `add` / `rm`
- [x] `config doctor` — 配置、连通性、console 发现、凭证有效性、License 一体化自检

### 4.2 `infini dash` — 看板（**P1 重点**）

REST 轨，`/api/ai/dashboards`：

- [x] `dash ls [--project]` → `GET /`
- [x] `dash show <id> [--spec]` → `GET /:id`，附带 filter / query / widget 摘要
- [x] `dash export <id> [-o file] [--spec-only] [--revision N]` → 导出可回写的 bundle
- [x] `dash import <spec.json>` → `POST /`（兼容裸 spec 与 bundle 两种输入）
- [x] `dash apply <id> <spec.json> [--change-brief]` → `PUT /:id`，带客户端 specHash 冲突检查
- [x] `dash rm <id>` → `DELETE /:id`
- [x] `dash widget ls <id>`、`dash widget rm <id> <widgetId>` → `DELETE /:id/widgets/:widgetId`
- [x] `dash revisions <id>` → `GET /:id/revisions`
- [x] `dash revision <id> <rev>` → `GET /:id/revisions/:revision`
- [x] `dash rollback <id> --revision N` → `POST /:id/rollback`
- [x] `dash layout get/set <id>` → `PATCH /:id/layout`，本地校验 12 栅格与 widget id
- [x] `dash layout commit <id>` → `POST /:id/layout/revision`
- [x] `dash query <id> [--query-ids ...] [--filter k=v]` → `POST /:id/query`，默认跑全部数据查询
- [x] `dash table-query <id> --widget ... --page N` → `POST /:id/table-query`
- [x] `dash filter ls <id>` → 从 spec 列出 filter 及传参示例
- [x] `dash filter options <id> <name>` → `POST /:id/filter-options`
- [x] `dash filter range <id> <name>` → `POST /:id/filter-range`
- [x] `dash refresh <id> [--wait]` → `POST /:id/refreshes`（202）+ 轮询到终态
- [x] `dash refresh status <id> <refreshId>` → `GET /:id/refreshes/:refreshId`
- [x] `dash refresh active <id>` → `GET /:id/refreshes/active`（无任务返回 null，非错误）
- [x] `dash refresh result <id> <refreshId> <queryId>` → `GET .../results/:queryId`
- [x] `dash refresh cancel <id> <refreshId>` → `DELETE /:id/refreshes/:refreshId`
- [x] `dash ask <id> [--widget]` → `POST /:id/ask`（生成上下文附件）

REST 轨实现要点：

- **filter 值按 spec 声明的类型转换**：`number` 转 JSON number、`daterange` 转 `{start,end}` 或 `{preset}`、多选 enum 转数组。靠文本形状猜类型两个方向都会错（`number` 拒绝字符串 `"10"`，`text` 拒绝数字 `10`），所以先读 spec 再转，未知 filter 名在本地就报错并列出合法名字。
- **`apply` 的冲突检查是客户端的**：REST `PUT` 的 DTO 不接受 `expected_spec_hash`（只有 Agent 轨的 `dashboard_submit` 有），所以 `export` 把 specHash 写进 bundle，`apply` 先比对再写，`--force` 可跳过。这挡住「两个人同时改一块看板」的常见窗口，不是严格的竞态保护。
- **`query` 默认排除 `filter_options` 查询**：这类查询只用于填充筛选下拉，query 接口会直接拒绝。
- **部分失败不算失败**：单个 query 失败在结果里逐条报告，只有全部失败才以退出码 1 结束。

Agent 轨：

- [x] `dash new --brief "..."` → `newTask`，**默认非交互**：一条 brief 让 Agent 建完，CI 友好
- [x] `dash new --guided --interactive` → 注入与 Web 端一致的引导提示词，走多轮确认
- [x] `dash edit <id> --brief "..."` → `newTask`，先 `dashboard_read` 再 patch；发起前先校验 `canWrite`
- [x] `dash chat <id> --question "..."` → `dash ask` 产出上下文后内联进 prompt，并挂上 pack 里的数据库与项目
- [x] `dash reply <taskId> [message] [--approve|--reject]` → `askResponse`，回答 Agent 的提问
- [x] `dash cancel <taskId>` → `cancelTask`
- [x] 流式渲染 `dashboard_read_result` / `dashboard_submit_result` 两类事件
- [x] `dashboard_submit` 被拒时把 `errors[]` 逐条可读化输出（含 spec 路径与错误码）

Agent 轨实现要点（`internal/agent`）：

- **必须先订阅 SSE 再发命令**：命令可能在后来的订阅者接上之前就跑完，那条结果就永久丢了。这条顺序有测试兜住。
- **`clientOperationId` 幂等 + `commandId` 跟踪**：一条 SSE 连接承载该用户的**全部**命令（含其他终端和浏览器标签），所以 `command.state` 事件按 `clientOperationId` 过滤，不是自己的不处理。
- **`state.ready` 后按 `GET /api/ai/state` 对账**：流可能被中途掐断，任务自身的状态才是权威，退出前统一回读一次。
- **模型参数可选但必须成对**：服务端只在传了其中一个时才校验配对，不传则沿用账号配置；`subAgentModelInheritMain=false` 时必须给出子 Agent 模型。这些在本地就拦。
- **提问一律结束当前流**：回答是同一任务上的一条新命令（新幂等键、新流），所以多轮循环放在 `Converse` 里，交互与非交互共用一条代码路径。

### 4.3 `infini task` — 任务

`/api/ai_task`：

- [x] `task ls` → `GET /list`；`task status <id...>` → `POST /statuses`
- [x] `task show <id>` → `GET /showTaskWithId/:id`；`task info <id>` → `GET /getTaskInfo/:id`；`task data <id>` → `GET /tasks?taskId=`
- [x] `task rm <id...>` → `POST /deleteTaskWithId`
- [x] `task cancel <id>` → `POST /cancelTask`
- [x] `task pin <id>` / `task unpin <id>` → `POST /setPinned`
- [x] `task graph <id>` → `GET /tasks/:taskId/notebook-graph`
- [x] `task sql <id> --view ...` → `POST /native-query-sql`（展开 `infini_ref` 得原生 SQL）
- [x] `task kpi-sql` → `POST /runKpiSql`
- [x] `task share set/get` → `POST /setShare`、`GET /shareStatus`
- [x] `task evidence` → `POST /toolEvidence`
- [x] `task msg <id>` → `GET /getUiMessageById`、`GET /messagePayload`
- [x] `task workspace <id>` → `GET /getTaskWorkspace/:id`
- [x] `task file ls/preview/get` → `GET /api/tools/taskFileTree/:taskId`、`POST /previewFile`、`GET /api/tools/storage/downloadTaskFile/:taskId`
- [x] `task zip <id>` → `GET /downloadZip`
- [ ] `task public *` → `publicTask`、`publicMessagePayload`、`publicToolEvidence`、`publicTaskFileTree`、`publicPreviewFile`、`publicDownloadTaskFile`、`publicDownloadZip`（挪到 P4，属分享/审计面）

#### task 轨实现要点（`internal/task`）

- **文件路由不在 `ai_task` 下**：`tools.module.ts` 用 `RouterModule` 把 upload/storage 挂到 `tools` 前缀，所以文件树是 `/api/tools/taskFileTree/:taskId`，下载是 `/api/tools/storage/downloadTaskFile/:taskId?path=`。之前 `api endpoints` 索引里的 `/api/storage`、`/api` 两条前缀是错的，已改。
- **`cancelTask` 是 POST 但 id 走 query**：控制器用 `@Query('taskId')`，body 留空；有测试钉住这一点，否则服务端收不到 id。
- **`runKpiSql` 的 `databases` / `tables` 是字符串不是数组**：DTO 上是 `@IsString()`，内容为 JSON 文本，所以 CLI 收 `--db` / `--table` 后自己 `json.Marshal` 成字符串再发。
- **列表只发已设置的筛选**：空 `task_name` 会变成对全部任务的模糊匹配，空值一律不上线；`audit` 按 web 的约定发 `1`，`project_ids` 逗号拼接，`--project` 自动推出 `project_filter=project`。
- **下载走流式落盘**：任务归档可能很大，`client.Download` 直接 `io.Copy` 到文件而不进内存；中途失败会删掉半个文件，因为截断的归档看起来像正常结果。

### 4.4 `infini agent` — Agent 命令全集

`POST /api/ai/message`，**24 种真实生效的 type 全覆盖**（原清单写的 30 种与当前服务端不符，见下方实现要点）：

- [x] `agent new` (`newTask`)、`agent reply` (`askResponse`)、`agent options` (`optionsResponse`)
- [x] `agent reply --approve` / `--deny` (`askResponse` + `yesButtonClicked` / `noButtonClicked`)
- [x] `agent resume` (`autoResumeTask`)、`agent load` (`showTaskWithId`)
- [x] `agent cancel` (`cancelTask`)、`agent clear [--all]` (`clearTask`，`--all` 服务端展开成 per-task)
- [x] `agent stop shell` (`stopShellSession`)、`agent stop tool` (`stopToolExecution`)
- [x] `agent stop subagent` (`killSubAgent`)、`agent stop job` (`killActiveJobs`，同步执行)
- [x] `agent snapshots` + `agent rollback [--message]` (`rollbackToSnapshot` / `rollbackAndSendMessage`)
- [x] `agent edit-first` (`editFirstMessageAndResend`)
- [x] `agent mode act|plan|graph|fast` (`updateChatMode`；`togglePlanActMode` 为旧别名，走 `agent send`)
- [x] `agent auto-approve` (`autoApprovalSettings`，读-改-写)
- [x] `agent resources` (`updateTaskResources`)、`agent tool-params` (`updateTaskToolParams`)、`agent engine` (`updateTaskEngine`)
- [x] `agent settings [--task]` → `POST /ai/settings`（任务级模型切换在服务端命令化为 `updateSettings`）
- [x] `agent browser takeover|resume|stop` (`browserTakeOver` / `browserResume` / `browserStop`)
- [x] `agent send <type>` 逃生舱：对照「服务端真正处理的 24 种」做客户端校验
- [x] `agent state [--task]` → `GET /ai/state`；`agent messages` 从同一 state 派生
- [x] `agent ping` → `GET /ai/ping`；`agent config` → `GET /ai/configuration`；`agent models` → `GET /ai/models`
- [x] `infini events [--task]` → `GET /ai/events` 原始事件流（调试用，P0 已实现）
- [x] 本版服务端**不存在**的 type 显式标注不适用：`summary_task_start` / `summary_task_stop` /
      `save_task_summary` / `exportTaskWithId` / `refreshOpenAiModels` / `selectImages` / `openImage` /
      `openSettings` / `openExtensionSettings` / `checkIsImageUrl`

#### agent 轨实现要点（`internal/agent`）

- **`/api/ai/message` 的 body 根本没被校验**：控制器签名是 `@Body() message: WebviewMessage`，而
  `WebviewMessage` 是 `import type` 来的纯 TS 类型，运行时已被擦除，Nest 的 `ValidationPipe` 拿不到
  metatype 就整个跳过。所以 DTO 里那份 `WEBVIEW_MESSAGE_TYPES`（18 项）**只是 Swagger 文档**。
  两个后果：① worker 处理但未列入枚举的 6 种（`autoResumeTask`、`showTaskWithId`、`updateSettings`、
  三个 `browser*`）实际可用；② 完全不存在的 type 会被接受并入队，然后在 worker 的 `default` 分支只留
  一行日志就丢掉，调用方看到的是 `{"queued": true}`——一个伪成功。因此 type 校验必须放在 CLI 侧，
  且在建 client 之前。
- **资源组的三态**：字段缺省 = 不动，`[]` = 清空，有值 = 替换。Cobra 的 `GetStringSlice` 对未设置的
  flag 返回空切片，直接透传就会把资源清空，所以 `Command` 里的 `databaseIds/ragIds/projectIds` 用
  `*[]string`，并按 `Changed()` 决定是否落字段。`engineId` 同理（空串 = 解绑）。
- **回滚点没有列表接口**：服务端在每个已提交的用户回合建快照，并按精确 `snapshot_ts` 查，对不上就
  `Snapshot <ts> not found`。所以 `agent snapshots` 从 `GET /ai/state` 的 `infiniMessages` 里筛
  `say=task`（首条）与 `say=user_feedback`（后续用户回合）推导出来——不这么做，用户只能去 Web UI 抄
  时间戳。
- **`autoApprovalSettings` 是整体覆盖写**：服务端存的就是收到的那个对象，少传一个字段就等于把那项
  能力关掉、把预算清零。所以 `agent auto-approve` 先读当前 state，再只覆盖用户传了的 flag；结构体
  全字段用指针，保证没提到的字段序列化时直接不出现。`databaseReturnLimit` 服务端会按部署上限 clamp。
- **create-only 模式**：`graph` / `fast` 只能在建任务时选。服务端确实拦，但拦在命令入队并被 worker
  取走之后，表现为一次「跑死的 run」而不是参数错误，所以 CLI 先拦。
- **`killSubAgent` 的 id 走 `text` 而不是 `taskId`**：`taskId` 用于路由到持有 lease 的 worker，而子
  Agent 活在父任务的 worker 内存里，服务端会自己从子任务 id 推出父任务。含 `_graph_` 的会被拒（图
  节点会话只读）。
- **`clearTask` 不带 `taskId` 不是一条命令**：控制器按该用户所有活跃 lease 展开成 per-task 命令，返回
  `commandIds` 数组而不是单命令信封——用户的任务可能散在多个 worker 上，单条命令只会被其中一个消费。
- **`killActiveJobs` 是唯一同步执行的 type**：杀引擎 job 需要调用方自己的 access token，控制器直接
  内联做掉，返回 `{success, notification}`。

### 4.5 `infini db` — 数据源

`/api/ai_database`：

- [x] `db ls` → `GET /list`
- [x] `db add` / `db update` → `POST /add`、`POST /update`
- [x] `db rm` → `POST /delete`
- [x] `db enable` / `db disable` → `POST /enabled`
- [x] `db test` → `POST /testConnection`
- [x] `db show <id|name>` → `GET /getDatabaseById/:id`、`GET /getDatabaseByName/:name`
- [x] `db schema <id>` → `GET /schema/:databaseId`
- [x] `db upload <id> <file>` → `POST /upload/:databaseId`（file 类型数据源）
- [x] `db bind-rag` / `db binds` → `POST /bindRags`、`GET /getBindRags/:databaseId`
- [x] `db review-list` → `GET /context-hub-review-databases`

### 4.6 `infini rag` — 知识库

`/api/ai_rag_sdk`：

- [x] `rag ls` / `rag ls --all` → `GET /`、`GET /all`
- [x] `rag show <id>` → `GET /:id`
- [x] `rag create` / `rag update <id>` → `POST /create`、`POST /update/:id`
- [x] `rag rm` → `POST /delete`
- [x] `rag enable` / `rag disable` → `POST /enabled`
- [x] `rag file ls/get/rm` → `POST /fileTree`、`POST /download`、`POST /deleteRemoteFile`
- [x] `rag bind-db` / `rag binds` → `POST /bindDatabases`、`GET /getBindDatabases/:ragId`

### 4.7 `infini project` — 项目与成员

`/api/ai_project`：

- [x] `project ls` / `create` / `update` / `rm` → `GET /list`、`POST /`、`PATCH /:projectId`、`DELETE /:projectId`
- [x] `project member ls/add/set/rm` → `/:projectId/members` 四个方法
- [x] `project tree` → `GET /:projectId/tree`
- [x] `project file preview/get/mv/cp/rm` → 对应 5 个文件路由
- [x] `project mkdir` → `POST /:projectId/directories`

#### db / rag / project 轨实现要点

- **`enabled` 不在数据源表上**：它落在 `ai_database_mapping`（按用户），`update` 服务端会把它解构丢掉，所以启停只能走 `POST /enabled`，是批量的、独立的命令。
- **`config` 与 `requiredExts` 的字符串化方向相反**：数据源 `config` 无论读写都是 JSON 文本；知识库 `requiredExts` 写入时是数组、读回时是 JSON 文本。CLI 两边都做转换。
- **`db update` / `rag update` 先读后写**：服务端 DTO 要求整条记录（rag 的 update 直接复用 create DTO），直接发部分字段会把没传的字段清空。所以先 GET 当前记录，再只覆盖用户传了的 flag。`project update` 是 PATCH，不需要这一步。
- **对象存储密钥只从环境变量读**：`rag file *` 的 `access_key_secret` 走 `INFINI_STORAGE_SECRET`，不提供 flag——命令行上的密钥会进 shell history 和进程列表。`--trace` 的脱敏名单也加了 `access_key_id` / `access_key_secret`。
- **绑定是替换不是合并**：`db bind-rag` / `rag bind-db` 发全量列表，不传任何 id 等于全部解绑，因此这一步有确认门。
- **`DELETE /:projectId/files` 带 body**：删文件/目录的路径在 body 里而不是 query，有测试钉住。
- **上传走流式 multipart**：`client.Upload` 用 `io.Pipe` 边读边发，数据源上传动辄几百 MB；中途读失败用 `CloseWithError` 让请求带错终止，而不是发出一个被静默截断的 body。

### 4.8 `infini hub` — Context Hub（语义层）

`/api/ai/context-hub`，端点最多的一组：

- [x] `hub memory start/batch/active/cancel/status/latest` → `memory-build/*` 7 个端点（含 `cancel-active`）
- [x] `hub table ls/add/update/rm` + `hub table dbs` → `table-data/*`
- [x] `hub column ls/add/update/rm` → `table-column/*`
- [x] `hub playbook ls/add/update/rm` → `playbook/*`
- [x] `hub kpi ls/add/update/rm` → `kpi/*`
- [x] `hub pref ls/add/update/rm` → `preference/*`
- [x] `hub draft add/ls/table-updates` → `draft/*`
- [x] `hub review pending/approve/reject/restore/translate` → `review/*`

#### hub 轨实现要点（`internal/hub`）

- **URL 段不是实体类型的机械变形**：`table_column_data` 的路由是 `table-column`、`user_preference` 是 `preference`。这是加新实体时最容易错的地方，`Kind.route()` 有测试逐条钉住。
- **`hub memory start` 的选表由 CLI 从 schema 推导**：服务端 DTO 要求 `tables[].columns[]` 且都不能为空，但那是 UI 勾选的结果。CLI 改为读 `GET /ai_database/schema/:id`，默认全选，`--table name` / `--table name:col1,col2` 收窄；`--missing-only` 用服务端自己给的 `inContext` 判断覆盖情况，比再去 hub 列表里对账可靠。显式选表优先于 `--missing-only`。
- **`taskId` 是构建工作区的名字**：服务端用它 `resolveStandaloneWorkspace(uid, taskId)`，格式无要求，所以 CLI 每次自己 mint 一个。
- **同一数据源同时只有一个构建**：`createPersistentJob` 遇到已有活跃 job 会直接返回它，所以重复 start 不会起两个。
- **批量和单个的选表位置不同**：`batch-start` 只收 `databaseIds`，选表在服务端做，所以批量命令不需要 schema 往返。
- **hub 没有 get-by-id 路由**：`update` 需要整条记录做覆盖，只能用 `search`（服务端的模糊匹配包含 id 列）当作查单条，收敛在 `findHubEntity` 这个泛型里。
- **审核可以只批准部分字段**：`approved_fields` 收窄范围，`field_values` 在写回前改值——这是「接受 AI 建议但要改一处」的正常路径，不是特例。空列表不能发：`approved_fields: []` 会被读成「什么都不批准」。
- **草稿列表必须带 `entity_type`**：不同实体的 payload 形状不同，端点不混着返回。

### 4.9 `infini skill` / `infini tool` / `infini rule` / `infini template`

- [x] `skill available/ls/state/install/uninstall/toggle/upload/edit/rm` → `/api/ai_skill/*` 9 个端点
- [x] `tool ls/local/installed/state/install/uninstall/toggle/upload/edit/rm` → `/api/ai_tool/*` 11 个端点（`state <pluginId>` 覆盖 `isInstalled/:pluginId`）
- [x] `rule ls/enabled/all/show/databases/add/update/toggle/rm` → `/api/ai_rule/*` 10 个端点
- [x] `template ls/show/create/update/rm` → `/api/ai_template/*` 5 个端点

实现要点（`internal/extension`）：

- **catalog 安装与本地上传是两套 id**：`uninstall` 收远端 catalog id，`rm`（`deleteLocal/:id`）收本地安装行 id。混用会得到 404 而不是有意义的报错，所以两个命令在帮助里互相指向。
- **skill / tool 的分页参数是 `pageNum`/`pageSize`**，与其它模块的 `page`/`pageSize` 不同，已用 wire 测试钉住。
- **`skill available` 是随上下文变化的**：服务端按任务已挂载的数据源类型 + 是否开浏览器过滤 catalog，所以同一账号对不同任务看到的列表不同。`--task` / `--browser` 就是这两个入参。
- **`editLocal` 的 zip 是可选的**：同一个 multipart 表单既能带新包也能只改元数据，`client.Upload` 因此支持空 filePath（只发字段、不发 file part）。
- **`tool install` 的展示元数据是必填**：服务端把 name/logo/alias/author 存进安装记录而不是回查 catalog。
- **`rule` 的两个 id 类型不通**：`enabled` 收数字数组、`delete` 收字符串数组，CLI 在 `rule toggle` 里先校验数字，避免 422。
- **`rule --database` 隐含 `rule_type=database`**：只有 database 类型会读 `databaseIds`，不一起设会静默无效。
- **`rule update` / `template update` 的 DTO 继承 create**：name/text 恒为必填，所以两个命令都先读后写，让单个 flag 真的只改一处。

### 4.10 `infini schedule` — 定时任务

`/api/ai_scheduler`（**不是 `ai_schedule`**，控制器挂载名与模块名不一致）：

- [x] `schedule ls` / `show` / `runs` → `GET /`、`GET /:id`、`GET /:id/runs`
- [x] `schedule create` / `update` → `POST /`、`PUT /:id`
- [x] `schedule pause` / `resume` → `PATCH /:id/pause`、`PATCH /:id/resume`
- [x] `schedule run` → `POST /:id/run`
- [x] `schedule archive` → `DELETE /:id`

实现要点（`internal/ops/schedule.go`）：

- **cron 是六段 Quartz**（秒 分 时 日 月 周），不是五段 Unix。五段会被服务端当字符串收下、之后才解析失败，所以 CLI 先数字段并给出 `"0 30 9 * * ?"` 的对照提示。
- **`taskConfig` 在保存时冻结，不在触发时解析**：定时任务会一直用创建时的模型和数据源，账号默认值变了也不跟随。这是刻意的，代价是改默认值不会更新已有计划，只能 `schedule update`。
- **`update` 与 `create` 共用 `SaveScheduleDto`**：title/prompt/cron/startAt 恒为必填，所以 `update` 先读后写、逐字段继承。
- **`resume` 会因「cron 没有未来可执行时间」而失败**——服务端拒绝启用一个永不会触发的计划，不是 bug。
- **`create` 的 `startAt` 默认取当前时刻**：任何其它默认值都会静默推迟首次执行。
- **run 记录带 `taskId`**，可直接接 `task show` / `agent` 追踪；`isMisfire` 表示重启后补发，而非按时触发。

### 4.11 `infini engine` / `infini runtime` — 引擎与运行时

- [x] `engine status/check/start/stop/ensure/logs` → `/api/infinity-sql/*` 6 个端点
- [x] `engine available` / `engine enabled` → `/api/ai_byzer/available`、`/getEnabledInfiniSQLEngine`
- [x] `runtime instances` → `GET /api/runtime/instances`
- [x] `runtime execution <taskId>` → `GET /api/runtime/tasks/:taskId/execution`
- [x] `runtime ready` / `drain` / `autoscaling` → **`/api/internal/*`**（不是 `/api/runtime/*`，属于另一个控制器）

实现要点（`internal/ops/engine.go`）：

- **「引擎」是两个不同的东西**：`/api/infinity-sql/*` 管的是本部署内嵌的单个引擎进程（全员共用，所以 `engine stop` 会打断所有人的查询，默认二次确认）；`/api/ai_byzer/available` 列的是账号可绑定的引擎，是 `agent engine` 和 `schedule --engine` 收的那个 id。
- **`runtime ready/drain/autoscaling` 在 `internal` 控制器下**，与 `runtime` 不同前缀。`drain` 的令牌走 `x-internal-token` 请求头，来源是 `INFINI_INTERNAL_TOKEN` 环境变量而非 flag；服务端只在配置了 `INTERNAL_HTTP_TOKEN` 时校验，所以未配置的部署会接受无鉴权 drain。
- **drain 打到哪个 worker 取决于请求落到哪个实例**：负载均衡端点下基本等于随机，要指定 worker 必须把 `--server` 指向具体实例，帮助里明说了这点。
- **`instances` 的 `status` 与 `effectiveStatus` 不是一回事**：心跳超时后 `effectiveStatus` 为 offline，而 `status` 仍是实例自己最后上报的值，表格两列都出。
- **`runtime execution` 是排查「任务卡住」的第一站**：任务不在 API 进程里跑，是 worker 拿 lease 执行，所以 API 日志干净而任务不动是正常现象。
- **`engine check` 用退出码表达结论**（运行中 0、未运行 3），可直接当 shell 卫语句。

### 4.12 `infini license` — 授权

- [x] `license status` → `GET /api/license/status`（公开，未登录可用）
- [x] `license refresh` → `POST /api/license/refresh`（同样公开）
- [x] `license limits` → `GET /api/license/limits`（需登录）
- [x] `agent new` 被限额拦截时，帮助与错误提示指向 `license limits`

实现要点：**synapse 自己不做授权判定**，只是把 proxy 的结论透传，所以这里失败通常意味着 proxy 不可达而非 license 无效——帮助文本里写明了。`status` 公开是刻意的：过期部署的登录页也得能说出自己过期了；`limits` 需要登录是因为配额按账号算。

### 4.13 `infini fs` — 文件与网盘

- [x] `fs ls/tree/mkdir/rmdir` → `/api/directories`、`/api/fileTree`、`/api/createDirectory`、`/api/deleteDirectory`
- [x] `fs put <dir> <file>` → `POST /api/upload/:directory`
- [x] `fs config` → `GET /api/uploadConfig`
- [x] `fs task-put <taskId> <file>` → `POST /api/taskUpload/:taskId`
- [x] `fs get <id>` → `GET /api/storage/download/:id`（`downloadTaskFile/:taskId` 已在 P2 的 `task file get`）
- [x] `fs rm` → `POST /api/storage/delete`
- [x] `fs session push/init/status/chunk/complete/cancel` → `/api/file_upload` 分片 5 步

实现要点（`internal/storage`）：

- **上传控制器挂在 API 根上**（`@Controller('/')`），所以这些路径没有模块前缀，`deleteDirectory` 还是「DELETE 带 body」。
- **分片是裸 body，不是 multipart**：服务端 `pipeline(req, writeStream)` 直接把请求体写进分片文件，套 multipart 会把边界写进分片、毁掉合并结果。为此给 client 加了 `Stream`，显式设 `ContentLength`（不设会走 chunked，服务端就没有尺寸可校验）。
- **分片会话的价值就是续传**：`uploadedChunks` 是服务端持有分片的权威清单，`fs session push --resume <id>` 只补缺的那些。读文件用 `ReadAt` 而非流式，因为重试必须能重读已经过去的分片。
- **会话还决定合并后干什么**：`postAction` 可以是 store / extract_archive / build_rag / import_database，所以「把 4 GB dump 导进数据源」走的是这条路，不只是搬文件。
- **init 后文件不能变**：`SendFile` 先比对 size，不一致直接报参数错误，否则会合出一个损坏文件、到 complete 才暴露。
- **任务上传走内容寻址仓库**：`--naming hash` 下相同内容幂等跳过，默认 original 同名覆盖。

### 4.14 `infini browser` — 浏览器自动化

`/api/ai_browser`：

- [x] `browser sessions` / `session [uid]` → 3 个查询端点
- [x] `browser go/click/input/scroll/key/find/view/move` → 8 个动作
- [x] `browser exec` / `browser console` → 只存在于通用入口的两个 console 动作
- [x] `browser raw <type> [payload]` → `POST /action` 通用入口
- [x] `browser direct <action> [payload]` → 保留对 `action/*` 8 个专用路由的可达性

实现要点（`internal/browser`）：

- **专用路由是诊断用的，通用入口才是超集**：`POST /action/navigate` 这一组把 sessionId 硬编码成 `'test-browser'`、不收 timeout，也到不了两个 console 动作。所以 CLI 所有命令都走 `POST /action`，`browser direct` 只为参数对等保留。
- **浏览器没连上是 HTTP 200 + `success:false`**，不是错误状态码。必须读 body 才知道有没有发生事情，`readResult` 因此以 body 为准并映射成非零退出码，未连接时额外提示去看 `browser session`。
- **未知 action 也是 200**（`Unknown action type: x`），所以 `CheckType` 在发请求前就拦掉。
- **session id 即浏览器标签页**：同一个 id 的动作作用在同一页上，navigate 之后 click 想打到同一页必须同 `--session`。
- **timeout 只能缩短**（100–60000ms），超出范围在本地报参数错误。
- 与 `agent browser takeover/resume/stop` 的分工：那组是把运行中 agent 的浏览器交出去/收回来，这组是运维直接操作浏览器。

### 4.15 `infini api` — 逃生舱（保证 100% 覆盖）

- [x] `infini-cli api <METHOD> <path> [--data @file|@-] [--query k=v] [--proxy] [--raw]` — 任意端点直调，带上当前 profile 的鉴权
- [x] `infini-cli api endpoints` — 内置端点索引，便于发现未包装的接口

这一条是"CLI 可执行所有操作"的兜底保证：新增后端接口在未包装成子命令前，也能立即通过 `infini api` 调用。

---

## 5. 分期执行顺序

### P0 — 骨架与地基 ✅

- [x] `go mod init`、目录结构、`Makefile`（build / cross / install / test / lint / clean）
- [x] 移植并改写 `internal/client`、`internal/output`、`internal/config`
- [x] 多 profile 配置 + `--profile` + 环境变量覆盖 + `INFINI_CONFIG` 路径隔离
- [x] 全局 flag：`--server` `--console` `--api-key` `--token` `--tenant-code` `--json` `--table` `--yes` `--timeout` `--lang` `--verbose` `--trace`
- [x] 统一输出协议与退出码（见 §6），服务端错误码 → 退出码 + 修复提示
- [x] `auth login` / `whoami` / `status` / `logout` / `mode`
- [x] `config ls|get|set|unset|path`、`config profile ls|use|add|rm`、`config doctor`
- [x] `infini-cli api` 逃生舱 + `api endpoints` 索引
- [x] `infini-cli events` SSE 订阅器（过滤、退避重连、鉴权失败不重试）
- [x] 配置优先级与 profile 行为的回归测试
- [x] 命令状态机 `internal/agent`（`clientOperationId` 幂等 + `commandId` 跟踪 + `state.ready` 对账）
- [ ] 针对真实部署的 `auth login` / `doctor` / `dash new` 端到端验证（需要本地 Infini + proxy 起服务）

### P1 — 看板（核心交付）

REST 运维轨与 Agent 创作轨**同期交付**。

- [x] §4.2 REST 轨全部命令（含 `dash export`、`dash widget ls`、`dash filter ls` 三个清单外的补充命令）
- [x] spec 本地往返（export → 编辑 → import / apply，带 `specHash` 冲突检测）
- [x] 看板刷新的长轮询（`--wait`，进度写 stderr，终态决定退出码）
- [x] filter 类型转换、请求报文与错误码映射的单元与集成测试
- [x] §4.2 Agent 轨 `dash new` / `dash edit` / `dash chat` / `dash reply` / `dash cancel`
- [x] 流式渲染 `dashboard_read_result` / `dashboard_submit_result`，`dashboard_submit` 被拒时逐条输出 `errors[]`
- [x] 命令状态机 `internal/agent`（订阅先行、幂等键、`command.state` 过滤、`state.ready` 对账），含假服务测试

### P2 — 资源与数据面

- [x] §4.3 task 全量（`public *` 一组挪到 P4，已完成）
- [x] §4.5 db 全量
- [x] §4.6 rag 全量
- [x] §4.7 project 全量
- [x] §4.8 hub 全量（含 KPI 与审核流）

### P3 — Agent 深度控制 ✅

- [x] §4.4 全部命令类型（**24 种**；原清单的 30 种含 10 个本版服务端不存在的 type，已核对并标注）
- [x] 审批/选项应答：`reply --approve/--deny`、`options`，`--interactive` 下 `Converse` 循环应答
- [x] 回滚三兄弟（`rollbackToSnapshot` / `rollbackAndSendMessage` / `editFirstMessageAndResend`）
      + `agent snapshots` 从 state 推导回滚点；`state.ready(forceReplace)` 对账沿用 P1 的 `run` 循环
- [x] 模型与子 Agent 模型切换的参数联动校验（`apiProvider`/`apiModelId` 必须成对，`subAgentModelInheritMain=false` 时子模型必填）
- [x] 模型 API key 只走 `INFINI_MODEL_API_KEY`，不提供 flag

### P4 — 运维与治理 ✅

- [x] §4.9 skill / tool / rule / template（35 个端点）
- [x] §4.10 schedule
- [x] §4.11 engine / runtime
- [x] §4.12 license
- [x] §4.13 fs（含分片续传）
- [x] §4.14 browser
- [x] 从 P2 挪来的 `task public *` 分享与审计一组（7 个端点）

`task public *` 的实现要点：**一套端点服务两类读者，而且不是一回事**。不带 `--audit` 是公开读，完全不需要凭据，但只对所有者 `task share` 过的任务有效——就是分享链接看到的那个视图。带 `--audit` 是合规读：需要 proxy 超管令牌，能读任意任务（无论是否分享），且每次读取都在服务端留下「谁读了什么」的日志。所以 audit 不是「带登录的公开读」，也不该被 CLI 在公开读被拒时自动启用，`--audit` 因此挂在每个子命令上而非命令组上，让它在使用点可见。

`task public evidence` 是这组的重点：分享出去的报告用消息时间戳引用证据，这个命令把引用还原成背后真实跑过的工具调用，读者可以核对一个数字而不是选择相信它。单次最多 100 条；报告用到了委派工作时要加 `--include-subagent`，因为子 agent 的证据在它自己的任务上。另外它是这组里唯一把 `audit` 放在 body 而不是 query string 的端点。

### P5 — 交付与分发 ✅

- [x] `infini-cli spec`（别名 `skill-spec`）— 输出 AI Agent 规范说明
- [x] 多平台交叉编译（linux/darwin/windows × amd64/arm64）
- [x] 版本清单 + `infini-cli update` 自更新（**独立分发通道，不复用 `plugins/infini_cli`**）
- [x] README + 命令参考文档
- [x] 单测（命令解析、参数校验、输出格式）
- [ ] 针对本地 Infini 的 e2e 冒烟（等环境，见下）
- [x] 私有化交付物：`scripts/release` 离线通道、`--audit-log`、`--dry-run`

`spec` 的实现要点：**命令清单是从 cobra 树里走出来的，不是手写的**。两百多条命令的手写清单撑不过一两个版本就会和二进制对不上，而一份说谎的规范比没有规范更糟。手写的是它周围的散文，因为 agent 会搞错的从来不是 flag 名字，而是那些约定：agent 的活儿是投递进队列而不是同步调用、省略资源列表和给空列表是两件事、密钥只走环境变量。

自更新的实现要点：**通道地址一个字都不写死**。私有化部署往往在自己的内网镜像上分发，甚至根本没有出网路由，写死公网前缀错的时候比对的时候多。通道来自 `update-channel` 配置项、`INFINI_UPDATE_CHANNEL` 或 `--channel`，三者都没有时 `update` 直接报错并告诉你怎么设。通道根目录放一份 `latest.json`（`{version, releasedAt, notes, artifacts:[{os,arch,url,sha256,size}]}`），`sha256` 是必填的——没有校验和的产物不值得装，宁可拒绝也不盲信。artifact 的 `url` 允许相对通道，所以做镜像只要把目录树拷过去，不用改清单里的任何东西。

替换自身用的是「先改名、再落位」：Windows 上正在运行的可执行文件不能被覆盖，但可以被改名，而且留着旧的意味着中途失败时还有一个能用的二进制可以退回去，而不是什么都不剩。装完会顺手删 `.old`，Windows 上旧映像还映射着时删不掉，那就留到下次更新再清。

`scripts/release` 是一个 Go 程序而不是 Makefile target：这边从 Windows 上切版本和从 CI 上切一样多，而 Go 工具链是两边都已经有的那个依赖。`go run ./scripts/release --version 1.4.0` 会编出六个平台并把 `latest.json` 写在旁边，产出目录本身就是一个可以直接拿去挂 web 服务的通道。

`--dry-run` 的实现要点：**只拦写，不拦读**。CLI 在写之前大多要先读一遍当前状态好把写做成 patch，一个查不了任何东西的 dry run 是没用的；被拦下的正好就是那些会改变什么的请求。被拦下的写会回一段自述（`{dryRun, method, path, body}`，走和 `--trace` 同一套脱敏），同时输出信封上多一个 `"dryRun": true`——否则被拦下的 create 仍然会解进命令的结果类型，打出一条字段全空的记录，看着像真发生过。

`--audit-log` 的实现要点：**审计写不下去就不发请求**。日志在写操作发出**之前**先探一次可写性，事后再记结果。反过来做的话，日志一旦写不了，就会静悄悄漏掉已经发生的请求，而一份有洞的审计比没有审计更糟，因为它看上去是完整的。

---

## 6. 全局交付基线

- **输出协议**：默认 JSON `{"success":bool,"data":any,"message":string}`；列表命令 `--table` 走表格；所有输出可 `jq`
- **退出码**：0 成功；1 业务错误；2 参数错误；3 鉴权失败；4 网络/服务不可达；5 License 限额拦截
- **错误可读化**：后端 `code` → 人类可读提示 + 修复建议（沿用 1101/1105 token 失效的处理思路，扩展到 License、租户、权限）
- **危险操作**：`rm` / `rollback` / `drain` / `archive` 默认二次确认，`--yes` 跳过，非 TTY 环境必须显式 `--yes`
- **幂等**：所有写操作生成 `clientOperationId`，重试复用
- **可观测**：`--verbose` 打印请求摘要，`--trace` 打印完整请求/响应（自动脱敏 api-key 与密码）

---

## 7. 决策记录

已定：

1. 二进制名 `infini`，module path `github.com/chaozwn/infini-enterprise-cli`
2. P1 看板 REST 轨与 Agent 轨同期交付
3. `dash new` 默认非交互（`--brief` 一次建完），`--interactive` 为可选引导模式
4. MCP server 模式（`infini mcp serve`）延后到 P5 之后再评估，当前不纳入范围

5. 自更新通道不写死任何地址：base URL 作为配置项 `update-channel`，可被 `INFINI_UPDATE_CHANNEL` 与 `--channel` 覆盖，未配置时 `update` 报错并提示怎么设。理由是私有化部署基本都用客户自己的内网镜像，写死公网前缀反而没用；顺带也就不存在和 `agent_infini` 的 `plugins/infini_cli` 混用的可能
6. 版本清单用通道根目录下的 `latest.json`：`{version, releasedAt, notes, artifacts:[{os,arch,url,sha256,size}]}`，`update` 校验 sha256 后原子替换自身

仍待定：

7. 是否把 `infini-cli` 与 `agent_infini` 共享的 HTTP/SSE 抽成独立 Go SDK 包。P5 评估结论：**暂不抽**。两边的认证模型（企业侧多租户 + proxy 超管令牌 vs 消费侧单账号）和错误映射已经分叉得足够远，抽出来的公共部分只剩一个薄薄的 `http.Do` 包装，换来的是一个要同步升级的跨仓依赖。继续各自维护，等两边真的出现第三个消费者时再议
