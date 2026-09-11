# infini-cli

面向 ToB / 私有化部署的 InfiniSynapse 命令行工具。目标是让 CLI 能执行 Infini 的所有操作，重点覆盖看板的创建与编辑。

与 [`infinisynapse-cli`](../infinisynapse-cli)（`agent_infini`，面向 ToC 与 AI Agent 场景）并行维护，两者不共享代码包，各自独立发版。

完整功能清单与实施进度见 [PLAN.md](./PLAN.md)。

## 当前状态

- **P0 地基** ✅ 配置与多 profile、认证、HTTP/SSE 客户端、退出码语义、`api` 逃生舱、`events` 事件流
- **P1 看板 REST 轨** ✅ 列表、查看、导出/导入、版本回滚、布局、查询、刷新、筛选、上下文附件
- **P1 看板 Agent 轨** ✅ 创建、编辑、提问、回答、取消
- **P2 任务** ✅ 列表/查看、状态、置顶、取消、删除、DAG、原生 SQL、KPI SQL、证据、消息、工作区文件与归档、分享
- **P2 资源面** ✅ 数据源（CRUD、连接测试、schema、上传、绑定）、知识库（CRUD、文档存储、绑定）、项目（CRUD、成员、文件）
- **P2 语义层** ✅ 记忆构建、表/列/Playbook/KPI/偏好 CRUD、草稿与审核流
- **P3 Agent 深度控制** ✅ 24 种命令类型全覆盖、审批/选项应答、回滚与改写首条、模式与资源/引擎/工具参数、能力开关、模型配置
- 技能/工具/规则/模板、定时任务、引擎与运行时、license、网盘、浏览器待做，期间可用 `infini-cli api` 直调

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

看板工作分两轨：**运维**（列表、查询、刷新、版本、布局）走 REST；**创作**（把业务需求变成一块看板）走 Agent，因为 spec 背后是一整张 Infini-SQL DAG 和筛选契约，由服务端校验和水合。

### 创建与编辑

```bash
infini-cli dash new --brief "近 30 天各渠道 GMV 与转化率趋势" --database db_sales
infini-cli dash edit dash_123 --brief "把转化率卡片换成折线图，并加上环比"
infini-cli dash chat dash_123 --question "为什么华东转化率比上月低？" --filter period=last_30d
```

**默认非交互**，为的是 CI 可预期：Agent 提问时命令会停下来把问题打出来，以退出码 1 结束，你再回答：

```bash
infini-cli dash reply <taskId> "用自然月，不要滚动 30 天"
infini-cli dash reply <taskId> --approve          # 审批类提问
```

想在终端里直接对话，加 `-i`；想让 Agent 先反过来梳理需求再动手，用 `--guided -i`（引导提示词与 Web 端一致）：

```bash
infini-cli dash new --brief "销售看板" --guided -i
```

进度流写 stderr，stdout 只有一份 JSON 结果，所以管道里解析不会被干扰。`--quiet` 关掉进度，`--reasoning` 打开思考过程，`--transcript` 把完整对话放进结果。

Agent 提交被服务端拒绝时，会逐条列出被拒的 spec 路径和错误码，而不是只说一句失败：

```
dashboard submit rejected with 2 error(s):
  widgets[0].query_id [UNKNOWN_QUERY] query q_x does not exist
  filters[1] [BAD_NAME] filter name must be snake_case
```

### 运维

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

## 任务

一个任务就是一次 Agent 对话，外加它产出的全部东西：查询构成的 notebook DAG、独立的工作目录、每个结果背后的工具证据。任务由 `dash new` 这类 Agent 命令创建，`task` 负责创建之后的一切。

```bash
infini-cli task ls --table
infini-cli task ls --status running --table
infini-cli task status t_1 t_2 --table          # 只读状态，批量轮询用这个，不拉对话
infini-cli task show t_1                         # 含完整对话
infini-cli task cancel t_1
infini-cli task pin t_1
```

### 结果溯源

一个数字是怎么算出来的，靠 DAG 和证据回答，不用重跑任务：

```bash
infini-cli task graph t_1                                 # 节点与依赖
infini-cli task sql t_1 --view kpi_monthly_sales           # 展开 infini_ref 得到原生 SQL
infini-cli task evidence t_1 --id ev_1 --include-subagent  # 工具调用的入参与出参
infini-cli task msg t_1 --ts 1736200000000                 # 列表里被截断的消息全文
```

`task kpi-sql` 可以在存进语义层之前先验证一条 KPI 的 SQL。源表按 `<库>.<表>` 传入，语句里用 `<库>_<表>` 引用，与服务端的注册约定一致：

```bash
infini-cli task kpi-sql --db chinook --table chinook.artists \
    --sql "SELECT count(*) FROM chinook_artists"
```

### 工作区文件

图表、导出、中间数据都落在任务自己的工作目录里：

```bash
infini-cli task file ls t_1 --files-only --table
infini-cli task file preview t_1 data/result.csv   # 解析后的预览，表格会按行返回
infini-cli task file get t_1 charts/sales.png --out ./downloads/
infini-cli task zip t_1 --out ./t_1.zip
```

下载是流式落盘，大归档不受内存限制；中途失败会删掉半个文件，因为截断的归档看起来像正常结果。

### 分享

```bash
infini-cli task share get t_1 --table
infini-cli task share set t_1 --public     # 需确认：任何拿到链接的人都能读
infini-cli task share set t_1 --private
```

## 数据源

连接配置的字段随 `--type` 变，CLI 原样透传给服务端校验。`db test` 可以在保存之前先验证一份配置：

```bash
infini-cli db ls --type mysql --table
infini-cli db add --name chinook --type sqlite --config '{"path":"/data/chinook.db"}'
infini-cli db test --type mysql --config @mysql.json
infini-cli db test --id db_1                    # 测已保存的那份
infini-cli db schema db_1                       # 表、列，以及语义层对它的了解
infini-cli db upload db_1 ./sales-2026.csv      # 仅 file 类型
```

`db update` 会先读当前记录、再只覆盖你传了的 flag——服务端的更新接口要整条记录，直接发部分字段会把没传的清空。

启停是单独的命令：它存在按用户的映射表里，不在数据源本身上，`update` 会忽略它。

```bash
infini-cli db enable db_1 db_2
infini-cli db disable db_1
```

## 知识库

文档本身留在原处：本地路径，或 OSS / S3 / COS 桶。把知识库绑到数据源，是让 Agent 用文档来理解这个库的表。

```bash
infini-cli rag ls --all --table
infini-cli rag create --name finance_docs --doc-dir /data/finance --ext pdf --ext md
infini-cli rag bind-db rag_1 --db db_1 --db db_2   # 替换而非追加，不传则全部解绑
infini-cli rag binds rag_1
```

文档存储按路径直接寻址，因为同一个目录可以撑起多个知识库。对象存储密钥只从 `INFINI_STORAGE_SECRET` 读，不提供 flag——命令行上的密钥会进 shell history 和进程列表：

```bash
export INFINI_STORAGE_SECRET=...
infini-cli rag file ls --fs oss --dir docs/ --endpoint oss-cn-hangzhou.aliyuncs.com --access-key AKID
infini-cli rag file get docs/q3.pdf --fs oss --access-key AKID --out ./downloads/
infini-cli rag file rm docs/stale.pdf --fs oss --access-key AKID   # 仅 oss/s3/cos 支持删除
```

## 语义层

Context Hub 是 Agent 写 SQL 之前读的东西：表和列的描述、分析 Playbook、KPI 定义、展示偏好。空的语义层只能靠猜，描述过的才能给出站得住的答案。

填充它最快的方式是记忆构建——读 schema 和样本数据，生成描述候选交给审核：

```bash
infini-cli hub memory start chinook --wait          # 选表由 CLI 从 schema 推导，默认全选
infini-cli hub memory start chinook --missing-only  # 只补语义层还没覆盖的表
infini-cli hub memory start chinook --table "invoices:id,total,date"
infini-cli hub memory batch db_1 db_2 --mode missing_only
infini-cli hub memory active --table
```

构建跑在服务端，CLI 退出不影响它。同一个数据源同时只有一个构建，重复 start 返回的是已经在跑的那个。

### 手工维护定义

```bash
infini-cli hub table ls --database db_1 --table
infini-cli hub table add chinook invoices --description "订单事实表，一行一个订单行"
infini-cli hub column add t_1 total --meaning "订单金额，含税，单位元"
infini-cli hub playbook add "月度复盘" --database chinook --content @playbook.md
infini-cli hub kpi add 月度销售额 --mode sql_playground --table chinook.invoices --sql @sales.sql
infini-cli hub pref add "金额显示" --value "金额保留两位小数，单位万元" --table chinook.invoices
```

KPI 的 `--mode` 决定哪个字段承载定义：`business_logic` 用 `--logic` 写文字口径，`sql_playground` 用 `--sql` 放语句。SQL 口径存进去之前，可以先用 `task kpi-sql` 验证。

### 审核

改别人拥有的数据源不会直接生效，而是产生一条草稿，所以审核是正常编辑路径的一环，不是管理员的额外工作：

```bash
infini-cli hub review pending --table
infini-cli hub draft ls table_data --status pending --table
infini-cli hub review approve table_data d_1
infini-cli hub review approve table_data d_1 --field table_description    # 只批准这一个字段
infini-cli hub review approve kpi d_2 --set kpi_description="季度口径已修正"  # 边批准边改值
infini-cli hub review reject kpi d_3 --comment "口径与财务对不上"
infini-cli hub review translate --to zh-CN --field table_description="Fact table of orders"
```

## Agent

`dash new` 这类命令是包了业务外壳的 Agent 调用；`agent` 是同一条通道的直接入口，Web UI 能让 Agent 做的事这里都能做。

```bash
infini-cli agent new "统计上季度各区域营收，输出一张表" --database db_1
infini-cli agent new "跑通全流程" --interactive          # 在终端里逐轮回答
infini-cli agent reply t_1 "用自然月口径"
infini-cli agent reply t_1 --approve                     # 或 --deny
infini-cli agent options t_1 '["华东","华南"]'
infini-cli agent messages t_1 --table
```

默认**不交互**：Agent 一提问就停下并把问题报出来，退出码非零。这是给 CI 用的——没人在旁边时挂着等回答，比直接失败更糟。要在终端里对话就加 `--interactive`。

### 停止与回滚

```bash
infini-cli agent cancel t_1                    # 停整个任务
infini-cli agent stop shell t_1 local:t_1:ab   # 只停一个 shell 会话
infini-cli agent stop job t_1 job_7            # 只停一个 SQL job
infini-cli agent stop subagent t_1_sub_2       # 只停一个子 Agent
infini-cli agent clear t_1                     # 释放运行时，对话仍留在库里
```

回滚要先知道能回到哪儿。服务端只在**已提交的用户回合**建快照并按精确时间戳查，所以 `snapshots` 先列出合法的 `--ts`：

```bash
infini-cli agent snapshots t_1 --table
infini-cli agent rollback t_1 --ts 1700000000000
infini-cli agent rollback t_1 --ts 1700000000000 --message "换个口径重来"
infini-cli agent edit-first t_1 "改成按渠道拆，不按区域"
```

带 `--message` 会顺带从那一点继续跑，所以它像一次普通回合那样流式输出；不带就只回滚，新状态随 SSE 到达。两者都是破坏性的，默认需要确认。

### 运行时配置

```bash
infini-cli agent mode plan --task t_1          # 不带 --task 则改账号默认
infini-cli agent resources t_1 --database db_1 --database db_2
infini-cli agent resources t_1 --rag ""        # 空值 = 清空该组
infini-cli agent engine t_1 --engine eng_1     # 或 --clear
infini-cli agent tool-params t_1 --tool tool_1=params.yaml
```

资源组有三态：**不传 = 不动，空值 = 清空，有值 = 替换**。`graph` 和 `fast` 只能在建任务时选，切不进已有任务，CLI 会在发命令前就拦住。

### 能力开关与模型

```bash
infini-cli agent auto-approve --table                    # 不带 flag 就是查看
infini-cli agent auto-approve --max-requests 500 --browser=false
infini-cli agent settings --provider openai --model gpt-4o
infini-cli agent settings --task t_1 --provider anthropic --model claude-sonnet-4
infini-cli agent models --table
```

`auto-approve` 是读-改-写：服务端存的就是收到的那个对象，少传一个字段等于把那项能力关掉，所以 CLI 先读当前值再只覆盖你传的那几个。模型 API key 只从 `INFINI_MODEL_API_KEY` 读，不提供 flag。

### 逃生舱

```bash
infini-cli agent send togglePlanActMode --task t_1 --field chatSettings='{"mode":"plan"}'
```

`send` 会校验 type。这一步不是多余的：`/api/ai/message` 的 body 在服务端根本没走校验（签名上的类型是 `import type` 来的，运行时已擦除），未知 type 会被接受、入队，然后在 worker 里只留一行日志就丢掉——调用方看到的是 `{"queued": true}`，一个伪成功。

## 项目

项目把任务、看板和文件圈到一组人身上，成员角色决定谁能改什么。角色有 `viewer`、`editor`、`manager`。

```bash
infini-cli project ls --table
infini-cli project create "销售分析" --description "季度复盘"
infini-cli project member add proj_1 user_7 --role editor
infini-cli project member ls proj_1 --table
infini-cli project tree proj_1
infini-cli project file get proj_1 data/result.csv --out ./downloads/
infini-cli project mkdir proj_1 data/raw
```

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

Agent 命令通道是异步的：`POST /api/ai/message` 只入队并立即返回，执行结果只通过 SSE 推送。所以 `dash new` 这类命令内部一律是「先订阅、再发命令、消费到终态、最后按 `GET /api/ai/state` 对账」—— 顺序不能反，命令可能在后来的订阅者接上之前就跑完，那条结果就永久丢了。

一条 SSE 连接承载该用户的全部命令（包括其他终端和浏览器标签发起的），所以 `command.state` 事件按 `clientOperationId` 过滤，不是本次运行的不处理。

`events` 是这条流的原始视图：

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
│   ├── dash*.go                 # 看板：CRUD、spec 往返、版本布局、查询、刷新、Agent 创作
│   ├── task*.go                 # 任务：生命周期、DAG 与证据溯源、工作区文件
│   ├── db.go                    # 数据源：CRUD、连接测试、schema、上传、绑定
│   ├── rag.go                   # 知识库：CRUD、文档存储、绑定
│   ├── project.go               # 项目：CRUD、成员、文件树
│   ├── hub*.go                  # 语义层：记忆构建、五类实体、草稿与审核
│   ├── agent*.go                # Agent 直控：会话、生命周期、回滚、运行时配置
│   ├── api.go                   # 任意接口直调逃生舱
│   ├── events.go                # SSE 事件流
│   ├── helpers.go               # 参数解析、JSON 载荷读取
│   ├── term.go                  # TTY 检测与无回显输入
│   └── version.go
└── internal/
    ├── agent/                   # 异步命令状态机、命令目录、SSE 流渲染、看板工具结果解析
    ├── auth/                    # proxy 登录链（md5 口令、JWT、profile）
    ├── cliexit/                 # 退出码与修复提示
    ├── client/                  # HTTP 封装、信封解包、错误分类、SSE、流式上传下载
    ├── config/                  # 多 profile 配置与优先级解析
    ├── dashboard/               # 看板 REST 封装、spec 模型、filter 类型转换
    ├── task/                    # 任务 REST 封装、文件树、流式下载
    ├── database/                # 数据源 REST 封装
    ├── rag/                     # 知识库 REST 封装、文档存储描述
    ├── project/                 # 项目 REST 封装
    ├── hub/                     # 语义层 REST 封装、记忆构建任务、审核请求
    └── output/                  # JSON / 表格输出
```
