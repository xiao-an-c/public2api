# public2api

多渠道账号池网关：把 **WorkBuddy 国内**、**WorkBuddy 国际**、**Grok** 三路账号，
包装成对外一套 OpenAI / Anthropic 兼容接口。

当前是**骨架阶段**：渠道目录、能力模型与漂移守卫已就位，适配器尚未接入。
`go run ./cmd/public2api` 打印能力矩阵与接入进度。

## 词汇

**渠道**（channel）、**账号类型**（kind）、**能力**（capability）、**漂移**（drift）
的确切含义见 [`CONTEXT.md`](./CONTEXT.md)。动这几个概念之前先读它。

最容易出的错是把「渠道」和「账号类型」混用：Grok 是**一个渠道**，它的网页版 /
Build / 控制台是**三种账号类型**。`grok_web` 不是渠道。

## 实现来源

三条链路要从相邻仓库**移植**，不是从零写。改动前先读来源，再决定搬什么：

| 账号类型 | 来源 | 搬什么 |
| --- | --- | --- |
| `wbp` | `~/wx/demo/workbuddy2api-panel` | `internal/upstream/`（27 文件 / 8,146 行）整体平移；`internal/pool/` 的选号与冷却逻辑 |
| `grok_web` | `~/wx/demo/grok2api/backend` | `internal/infra/provider/web/`（18 文件 / 9,474 行）——最难，含私有 WebSocket + Cloudflare |
| `grok_build` | `~/wx/demo/grok2api/backend` | `internal/infra/provider/cli/`（26 文件 / 9,228 行）——设备码 OAuth + 原生 Responses |
| `grok_console` | `~/wx/demo/grok2api/backend` | `internal/infra/provider/console/`（12 文件 / 3,442 行）——最易，粘凭据即用 |
| 三条共用 | `~/wx/demo/grok2api/backend` | `internal/infra/provider/conversation/`（协议转换）、`searchresult/`（信源消毒）、`internal/infra/egress/`（出口治理，3,360 行） |

**不搬** grok2api 的 `internal/application/`、`internal/transport/`、
`internal/infra/persistence/`——那是它自带的网关层（约 49,000 行），与本仓库重复。
理由与实测行数见 [`docs/plan-public2api.md`](./docs/plan-public2api.md) §1.3。

## 硬约束

**凭据加密后落库。** 明文只存在于适配器发请求前的短暂窗口；`Credential` 的
`String`/`GoString` 已改写，日志里只会出现 `Credential{kind:..., fields:N}`。
密钥首次写入账号后不可更换——密文没有密钥版本号。

**加能力改两处，漂移守卫核对。** 一处是 `internal/channel/catalog.go` 的声明，
一处是适配器的能力接口实现。两者不一致时 `Registry.Drift` 会在装配期报出来：
声明了没实现 = 面板上有个点了没反应的按钮；实现了没声明 = 功能在但没入口能到达它。

**面板菜单从能力派生。** 渠道维护空间里的二级菜单是 `Channel.Menu()` 的输出，
不是前端配置。加渠道、加菜单项都不该改前端——需要改前端就说明抽象漏了。

## 校验

```sh
go test ./...
go run ./cmd/public2api -check   # 有漂移时非零退出，CI 用
```
