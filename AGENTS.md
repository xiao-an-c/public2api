# public2api

多渠道账号池网关：把 **WorkBuddy 国内**、**WorkBuddy 国际**、**Grok** 三路账号，
包装成对外一套 OpenAI / Anthropic 兼容接口。

## 词汇

**漂移**、**渠道**、**账号类型**、**能力** 的定义与边界见
[`CONTEXT.md`](./CONTEXT.md)——动这几个概念之前先读它。

## 实现来源

四条链路从相邻仓库**移植**，改动前先读来源：

| 账号类型 | 来源 | 搬什么 |
| --- | --- | --- |
| `wbp` | `~/wx/demo/workbuddy2api-panel` | `internal/upstream/`（27 文件 / 8,146 行）整体平移；`internal/pool/` 的选号与冷却 |
| `grok_web` | `~/wx/demo/grok2api/backend` | `internal/infra/provider/web/`（18 文件 / 9,474 行）——最难，私有 WebSocket + Cloudflare |
| `grok_build` | `~/wx/demo/grok2api/backend` | `internal/infra/provider/cli/`（26 文件 / 9,228 行）——设备码 OAuth + 原生 Responses |
| `grok_console` | `~/wx/demo/grok2api/backend` | `internal/infra/provider/console/`（12 文件 / 3,442 行）——最易，粘凭据即用 |
| 三条共用 | `~/wx/demo/grok2api/backend` | `provider/conversation/`（协议转换）、`provider/searchresult/`（信源消毒）、`internal/infra/egress/`（出口治理） |

搬什么的判据见 [`docs/plan-public2api.md`](./docs/plan-public2api.md) §1.3。

## 硬约束

**凭据加密后落库。** 明文只存在于适配器发请求前的短暂窗口；`Credential` 的
`String` / `GoString` 已改写，日志里只会出现 `Credential{kind:..., fields:N}`。
密钥首次写入账号后不可更换——密文没有密钥版本号，换钥即丢号。

**加能力改两处。** `internal/channel/catalog.go` 的声明，加适配器的能力接口实现。
`TestNoDrift` 会在 `go test` 时核对两者一致。

**面板菜单从能力派生。** 渠道维护空间的二级菜单是 `Channel.Menu()` 的输出，
不是前端配置。加渠道、加菜单项都不该改前端——需要改前端就说明抽象漏了。
