# Error-Tracer

[English](README.md)

Error-Tracer 是一个轻量、自托管的浏览器错误收集器。当前服务使用 Go
编写，将聚合后的问题和有界事件历史存储在 SQLite 中，并提供零依赖浏览器
SDK 和内嵌的问题处理 Dashboard。

2013 年的 PHP/MySQL 原始实现保存在
[`v1.0.0-legacy`](https://github.com/soulteary/Error-Tracer/tree/v1.0.0-legacy)
标签中，不属于当前运行时。

## 功能

- 捕获浏览器运行时错误、未处理的 Promise 拒绝和资源加载失败。
- 持久化之前统一规范事件，并移除 URL 中的凭据、查询参数和片段。
- 使用稳定的 SHA-256 指纹聚合同类事件。
- 使用 SQLite 存储问题聚合数据。
- 为每个问题保留最新事件，并支持稳定的游标分页。
- 在单个事务中原子写入最多 100 个事件，任一步失败都会回滚整批数据。
- 支持 `open`、`resolved` 和 `ignored` 三种问题状态。
- 相同指纹再次出现时自动重开已解决问题，同时保持已忽略问题的状态。
- 提供带鉴权的 JSON API 和内嵌 Dashboard。
- Dashboard 支持 English / 简体中文，且不依赖浏览器存储。
- 提供显式开启、完全隔离真实数据库的公开只读演示模式。
- 使用 `error-tracer demo` 无配置、无数据库启动产品演示。
- 限制请求体大小、浏览器来源、单个对等端速率，并使用常量时间比较凭据。
- 可构建为单个静态二进制，也可在非 root、只读容器中运行。
- 自带数据库/程序基准测试和有安全边界的 HTTP 压力测试命令。

## 无配置查看演示

在源码目录中，只需一条命令即可启动产品演示：

```sh
go run ./cmd/error-tracer demo
```

打开 <http://127.0.0.1:8080/>，Dashboard 会立即进入内置只读样例工作区。
该命令不要求凭据、不打开 SQLite，也不会注册事件采集和管理问题路由。

v2.0.0 发布后，无需下载源码也可以启动同一个演示：

```sh
docker run --rm --pull=always --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges:true \
  -p 127.0.0.1:8080:8080 ghcr.io/soulteary/error-tracer:2 demo
```

进程启动时会输出可直接打开的演示 URL。每个
[GitHub Release](https://github.com/soulteary/Error-Tracer/releases) 都会附带
Linux、macOS、Windows 预编译文件，以及校验和、SBOM 和来源证明。启动演示前可用
`error-tracer version` 确认二进制版本。

## 使用 Docker Compose 快速启动

复制环境变量模板：

```sh
cp .env.example .env
```

生成两个彼此独立的随机凭据，并写入 `.env`：

```sh
openssl rand -hex 16
openssl rand -hex 24
```

第一个值可用于 `ERROR_TRACER_INGEST_KEY`，第二个值可用于
`ERROR_TRACER_ADMIN_TOKEN`。将 `ERROR_TRACER_ALLOWED_ORIGINS` 设置为每个
允许提交事件的浏览器应用的精确来源，例如 `https://app.example.com`。

启动服务：

```sh
docker compose up --build -d
curl --fail http://localhost:8080/readyz
```

打开 <http://localhost:8080/>，输入管理员令牌连接。Dashboard 只把令牌保留
在当前标签页的内存中。使用页面上的语言选择器，或直接打开
`http://localhost:8080/?lang=zh-CN` 切换到简体中文；语言选择只体现在 URL
中，不会写入浏览器存储。

Compose 使用名为 `error-tracer-data` 的卷保存 `error-tracer.db`。

如果还没有真实事件，可参考[演示模式](docs/demo.md)开启内置只读样例。

## 浏览器 SDK

服务在 `/assets/error-tracer.js` 提供内嵌 SDK：

```html
<script src="https://errors.example.com/assets/error-tracer.js"></script>
<script>
  const tracer = ErrorTracer.init({
    endpoint: "https://errors.example.com/api/v1/events",
    projectKey: "替换为采集密钥",
    release: "web@2026.08.29",
    environment: "production",
    tags: { region: "ap-southeast-1" },
  });

  tracer.captureMessage("checkout started");
</script>
```

默认自动捕获错误。如果只需要手动上报，可设置 `autoCapture: false`。客户端还
会先在内存中排队，并在累计 10 个事件、等待 1 秒或页面隐藏时，将事件发送到
原子批量接口。队列最多保留 100 个事件；失败的批次采用指数退避，最多重试
2 次。可通过 `batchSize`、`flushInterval`、`maxQueueSize`、`maxRetries`、
`retryBaseDelay` 和 `maxBatchBytes` 调整这些边界。

在可控的页面关闭流程中，可调用 `await tracer.flush()` 并检查返回值；
`tracer.getStats()` 可查看排队、成功、重试、失败和丢弃数量。
`captureMessage` 或 `captureException` 成功只表示未满批次已进入本地队列，
`flush()` 才表示本轮所有批次是否都被传输层接受。客户端还支持
`sampleRate`、`maxEventsPerMinute`、`beforeSend`、`batchEndpoint` 和自定义
批量 `transport`。事件进入队列前，可使用 `beforeSend` 删除业务特有的敏感值。

## 采集 API

### 单个事件

`POST /api/v1/events` 接受 JSON，也接受包含 JSON 的 `text/plain`，以便 SDK
使用 `navigator.sendBeacon`：

```json
{
  "project_key": "替换为采集密钥",
  "event": {
    "kind": "error",
    "message": "TypeError: value is not a function",
    "stack": "TypeError: value is not a function\n    at checkout (app.js:10:2)",
    "source_url": "https://app.example.com/app.js?build=42",
    "page_url": "https://app.example.com/checkout?session=secret",
    "line": 10,
    "column": 2,
    "occurred_at": "2026-08-29T14:00:00Z",
    "release": "web@2026.08.29",
    "environment": "production",
    "tags": {
      "feature": "checkout"
    }
  }
}
```

服务端会覆盖 `id`、`received_at` 和 `user_agent`。成功时返回
`202 Accepted`，以及服务端生成的事件 ID 和问题指纹。

### 原子批量写入

`POST /api/v1/events/batch` 接受 1–100 个事件，解码后的请求体最大为
1 MiB。所有事件都会在修改 SQLite 之前完成规范化和校验，之后的全部
UPSERT 在同一个事务中执行：

```json
{
  "project_key": "替换为采集密钥",
  "events": [
    {
      "kind": "error",
      "message": "first failure"
    },
    {
      "kind": "unhandled_rejection",
      "message": "second failure"
    }
  ]
}
```

响应会按输入顺序返回每个事件的服务端 ID 和指纹。如果校验、UPSERT、
事务内读回或提交失败，整批数据都不会持久化。

## 问题 API

问题接口要求管理员令牌：

```http
Authorization: Bearer 替换为管理员令牌
```

| 方法 | 路径 | 用途 |
| --- | --- | --- |
| `GET` | `/api/v1/issues?limit=50` | 按最新出现时间读取第一页 |
| `GET` | `/api/v1/issues?limit=50&cursor=...` | 使用 `next_cursor` 继续读取 |
| `GET` | `/api/v1/issues?status=open` | 按 `open`、`resolved` 或 `ignored` 筛选 |
| `GET` | `/api/v1/issues/{fingerprint}` | 读取单个问题 |
| `GET` | `/api/v1/issues/{fingerprint}/events` | 按最新顺序读取保留的事件 |
| `PATCH` | `/api/v1/issues/{fingerprint}` | 修改问题状态 |

状态修改请求体：

```json
{
  "status": "resolved"
}
```

相同指纹的新事件会自动将 `resolved` 问题恢复为 `open`；`ignored`
问题再次出现时仍保持忽略状态。

每页最多返回 100 个问题。存在下一页时，响应会包含不透明的 `next_cursor`；
继续请求时应保持相同的 `limit` 和 `status` 筛选。游标不能与偏移量同时使用。
为兼容旧客户端，`offset` 参数仍然保留，但超过 100,000 的偏移量会被拒绝；
内嵌 Dashboard 已改用游标分页。

事件历史分页同样接受 `limit` 和 `cursor`。`total` 表示当前保留的事件数，
问题上的 `occurrences` 仍表示生命周期累计次数。写入新事件的同一事务会删除
超出每问题上限的最旧历史记录。

## 服务端点

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/` | 无 | Dashboard 外壳；读取真实数据仍需管理员令牌 |
| `GET` | `/assets/error-tracer.js` | 无 | 内嵌浏览器 SDK |
| `GET` | `/api/v1/meta` | 无 | 公开服务版本及演示模式标记 |
| `GET` | `/api/v1/demo/issues` | 无，仅演示模式 | 列出内置只读演示问题 |
| `GET` | `/api/v1/demo/issues/{fingerprint}` | 无，仅演示模式 | 读取一个内置演示问题 |
| `GET` | `/api/v1/demo/issues/{fingerprint}/events` | 无，仅演示模式 | 读取内置事件历史 |
| `POST` | `/api/v1/events` | 请求体中的采集密钥 | 提交单个事件 |
| `POST` | `/api/v1/events/batch` | 请求体中的采集密钥 | 原子提交一批事件 |
| `GET` | `/api/v1/issues` | 管理员 Bearer 令牌 | 列出问题 |
| `GET` | `/api/v1/issues/{fingerprint}` | 管理员 Bearer 令牌 | 读取问题 |
| `GET` | `/api/v1/issues/{fingerprint}/events` | 管理员 Bearer 令牌 | 读取保留的事件 |
| `PATCH` | `/api/v1/issues/{fingerprint}` | 管理员 Bearer 令牌 | 更新状态 |
| `GET` | `/healthz` | 无 | 进程存活状态 |
| `GET` | `/readyz` | 无 | 是否可以接收新工作，包括实时 SQLite 读取探测 |
| `GET` | `/metrics` | 无，启用后可用 | 低基数 Prometheus 指标 |

## 配置

| 变量 | 必需 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `ERROR_TRACER_ADDRESS` | 否 | `:8080` | HTTP 监听地址 |
| `ERROR_TRACER_DATABASE_PATH` | 否 | `error-tracer.db` | SQLite 数据库路径 |
| `ERROR_TRACER_SQLITE_MAX_OPEN_CONNECTIONS` | 否 | `4` | SQLite 连接池大小，范围 1–32 |
| `ERROR_TRACER_MAX_EVENTS_PER_ISSUE` | 否 | `100` | 每问题保留的最新事件数，范围 1–1,000；调低后会在启动时裁剪既有历史 |
| `ERROR_TRACER_PROJECT_ID` | 否 | `default` | 当前进程拥有的项目命名空间 |
| `ERROR_TRACER_INGEST_KEY` | 是 | — | 采集凭据，至少 16 字节 |
| `ERROR_TRACER_ADMIN_TOKEN` | 是 | — | 管理凭据，至少 24 字节 |
| `ERROR_TRACER_ADMIN_TOKEN_PREVIOUS` | 否 | 空 | 轮换期间临时接受的旧管理员令牌 |
| `ERROR_TRACER_ALLOWED_ORIGINS` | 否 | 空 | 逗号分隔的精确 HTTP(S) 浏览器来源 |
| `ERROR_TRACER_METRICS_ENABLED` | 否 | `false` | 在 `/metrics` 开放无鉴权 Prometheus 指标 |
| `ERROR_TRACER_RATE_PER_MINUTE` | 否 | `120` | 每个直接对等端每分钟允许的采集请求数 |
| `ERROR_TRACER_RATE_BURST` | 否 | `30` | 令牌桶最大突发量 |
| `ERROR_TRACER_RETENTION_DAYS` | 否 | `0` | 删除超过指定天数未再次出现的问题；`0` 表示禁用清理 |
| `ERROR_TRACER_DEMO_MODE` | 否 | `false` | 开放隔离的公开只读演示 |

`ERROR_TRACER_PORT` 只用于 Compose 的宿主机端口，默认值为 `8080`。来源
白名单为空时，带 `Origin` 的浏览器采集会被禁用；不发送 `Origin` 的非浏览器
客户端仍可提交事件。

启用保留策略后，Error-Tracer 会在启动时清理一次，之后每 24 小时清理一次。
清理仅作用于当前项目、依据 `last_seen` 判断，并且每个事务最多删除 500 条；
恰好位于截止时间的问题会被保留。SQLite 会复用删除后释放的页面。如果必须立即
缩小数据库文件，请在停机状态下另行执行 `VACUUM`。删除过期问题时也会级联删除
其保留的事件记录。

### SQLite 并发与备份

默认的四连接池会启用 SQLite WAL 模式。SQLite 仍然只有一个写入者，但 Dashboard
和 API 读取可以在写事务提交期间继续。每个池内连接都会设置 5 秒忙等待和外键检查。
将 `ERROR_TRACER_SQLITE_MAX_OPEN_CONNECTIONS=1` 可恢复完全串行的回滚日志模式；
内存数据库也必须使用该设置。WAL 会产生 `-wal` 和 `-shm` 辅助文件，因此应使用
理解 SQLite 的备份工具，或停服后再复制数据库文件。数据库应放在锁语义可靠的本地
文件系统，而不是无法安全支持 WAL 的网络文件系统。

服务会记录有序的 SQLite 架构版本。启动时会使用即时写事务串行执行版本检查和
所有待执行迁移，避免并发进程重复应用同一项 DDL。现有未记录版本的数据库会被
接管，已有问题数据不会丢失。运行新版 Error-Tracer 前应先备份数据库；如果数据库
架构版本高于当前程序支持的版本，程序会拒绝启动。

从只保存聚合数据的旧数据库升级时，事件历史初始为空；升级后的新事件会被保留，
但无法从聚合行还原升级前的每次事件。启动时还会按照
`ERROR_TRACER_MAX_EVENTS_PER_ISSUE` 裁剪所有既有历史；删除已保留的载荷不会改变
问题的累计发生次数。

就绪接口会实时读取 SQLite 所需架构；存储或必需表不可用时返回 `503`。Prometheus 输出
同时提供综合状态 `error_tracer_ready` 和最近一次存储探测结果
`error_tracer_store_ready`。可使用内置维护命令执行完整的快速一致性检查或创建
在线一致快照：

```sh
ERROR_TRACER_DATABASE_PATH=/data/error-tracer.db error-tracer db check
ERROR_TRACER_DATABASE_PATH=/data/error-tracer.db \
  error-tracer db backup /backups/error-tracer.db
```

这两个命令不需要采集密钥或管理员令牌，并以只读方式打开源数据库。备份会包含
已经提交到 WAL 的数据，在发布前检查新快照，并拒绝覆盖已有目标文件。

### 无访问空窗地轮换管理员令牌

1. 生成一个新的随机令牌。
2. 将 `ERROR_TRACER_ADMIN_TOKEN` 设置为新值，并临时将旧值放入
   `ERROR_TRACER_ADMIN_TOKEN_PREVIOUS`。
3. 重启 Error-Tracer，将 Dashboard 或 API 客户端迁移到新令牌。
4. 清空 `ERROR_TRACER_ADMIN_TOKEN_PREVIOUS`，再次重启。

重叠期间两个令牌都拥有完整管理权限。服务端会为每个请求检查所有已配置候选，
并拒绝空值以外的短旧令牌或重复令牌。该机制只用于安全轮换，并不等同于基于角色
的访问控制。

## 本地开发

模块声明 Go 1.27。运行浏览器 SDK 测试和文档检查时需要 Bun 1.4.0 或更高版本。

```sh
go mod verify
go vet ./...
go test ./...
go test -race ./...
bun test
bun run check:docs
```

从源码运行：

```sh
ERROR_TRACER_INGEST_KEY=development-key-1 \
ERROR_TRACER_ADMIN_TOKEN=development-admin-token-1 \
go run ./cmd/error-tracer
```

构建静态服务二进制：

```sh
CGO_ENABLED=0 go build -trimpath -o error-tracer ./cmd/error-tracer
```

基准和压力测试不会随普通单测自动运行。完整命令、指标解释和安全限制见
[性能与压力测试](docs/performance.md)。

## 文档

- [变更日志](CHANGELOG.md)
- [参与开发](CONTRIBUTING.zh-CN.md)
- [演示模式及其安全边界](docs/demo.md)
- [性能基准与压力测试](docs/performance.md)
- [维护者发布流程](docs/releasing.zh-CN.md)
- [安全策略](SECURITY.md)
- [English README](README.md)

## 部署注意事项

- 采集密钥和管理员令牌必须独立、随机生成。
- 旧管理员令牌只应在轮换窗口内临时保留。
- 接收网络中的浏览器流量前，应将服务置于 HTTPS 之后。
- 浏览器来源必须精确配置；系统有意拒绝通配符。
- 限流器使用直接 TCP 对等端，不信任转发地址请求头。
- 将 SQLite 路径或 `/data` 卷持久化到容器生命周期之外。
- 启用 `/metrics` 后，应在反向代理或网络层限制其访问范围。
- 优雅关闭 HTTP 服务前，进程会先将自身标记为未就绪。
- 不需要公开演示时，保持 `ERROR_TRACER_DEMO_MODE=false`。

## 许可证

Error-Tracer 使用 [Apache License 2.0](LICENSE) 许可证。
