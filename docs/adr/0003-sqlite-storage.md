# 3. 存储用 SQLite（纯 Go 驱动），保留 JSON 导入导出

日期：2026-09-25
状态：已接受

## 背景

底子 `workbuddy2api-panel` 用 JSON 文件：`auths/<uid>.json` 一账号一文件，
加一个 `data/state.json` 存池状态。单渠道、几十个账号时够用。

多渠道路由之后会撞上三堵墙：

1. **按渠道统计**要扫全部文件再内存聚合；
2. **跨渠道选号**要在内存里拼多个来源；
3. **并发写入**靠文件锁，多渠道后台任务同时跑时锁竞争会变复杂。

计划 §12 里这条已经定过（用户选择「换成 SQLite，保留 JSON 导入导出」）。

## 决策

**主存储用 SQLite**，表结构见 `internal/store/schema.go` 的迁移 1。

**驱动用 `modernc.org/sqlite`**（纯 Go 实现），不用 `mattn/go-sqlite3`。

**保留 JSON 导入导出**作为迁移与备份通道：首次启动检测到 `auths/` 且 `accounts`
表为空时自动迁入，且旧文件保留不动（可回滚）。面板提供导出按钮。

**账号表用单表 + 渠道/类型列**，不是每渠道一张表——这样「按渠道统计」是普通
`WHERE`，「跨渠道选号」是普通 `IN`，不需要 UNION。

**冷却表把账号级与账号×模型级合并**：主键是 `(channel, account_id, model)`，
`model` 为空串表示账号级。两级本来就该并存——一个账号可能整体可用、但某个模型
还在限额——所以让它们在同一个主键空间里共存，而不是两张表。

## 后果

**选纯 Go 驱动的理由**：交叉编译不需要 CGO 工具链，`GOOS=linux go build` 直接出
单二进制，与底子和 `grok2api` 的部署形态一致（两者都是单二进制 + 只读配置挂载）。
代价是 `modernc.org/sqlite` 比 cgo 版慢一些，且依赖树更大
（引入 `modernc.org/libc` 等 9 个间接依赖）——对账号池网关的负载量级可以接受。

**代价**：
- SQL 方言被 SQLite 绑住。将来若要上 Postgres 做多实例，`internal/store` 的查询
  要重写（表结构基本可平移，但 `strftime`、`INSERT OR REPLACE` 这类要改）。
  `codex2api` 用的是双驱动（SQLite / PG），本仓库暂不做，先把单机做扎实。
- 迁移必须**只增不改**：`migrations` 里已发布的条目不可修改，改结构就加新的一条。
  `TestLatestVersionMatchesMigrations` 守这条（version 必须从 1 起严格递增）。
- 迁移**不做事务**是不可接受的，所以 `DB` 接口包含 `BeginTx`，每条迁移跑在自己的
  事务里，中途失败整条回滚。

**环境事实**：本机 `proxy.golang.org` 不可达（EOF），拉依赖需要
`GOPROXY=https://goproxy.cn,direct`。已写入 `.env` 前的操作习惯——
换机器时先确认这一点，否则 `go mod tidy` 会失败。
