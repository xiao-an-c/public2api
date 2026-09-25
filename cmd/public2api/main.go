// Command public2api 是多渠道账号池网关。
//
// 当前处于**骨架阶段**：渠道目录、能力模型、漂移守卫、账号模型与存储层已就位，
// 但四条链路（wbp / grok_web / grok_build / grok_console）都还没有适配器。
// 所以对外只有**只读**端点——渠道目录与健康检查；对话端点要等第一条适配器接入。
//
//	public2api              启动 HTTP 服务（默认 :7863）
//	public2api -matrix      打印渠道能力矩阵后退出
//	public2api -matrix -check   同上，但有漂移时以非零码退出
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/xiao-an-c/public2api/internal/adapter"
	"github.com/xiao-an-c/public2api/internal/channel"
	"github.com/xiao-an-c/public2api/internal/server"
	"github.com/xiao-an-c/public2api/internal/store"
)

func main() {
	addr := flag.String("addr", ":7863", "HTTP 监听地址")
	matrix := flag.Bool("matrix", false, "打印渠道能力矩阵后退出")
	check := flag.Bool("check", false, "与 -matrix 同用：有漂移时以非零码退出")
	flag.Parse()

	reg, err := channel.NewRegistry(channel.Catalog()...)
	if err != nil {
		fmt.Fprintf(os.Stderr, "渠道目录自检失败：%v\n", err)
		os.Exit(1)
	}

	providers := providerDeclarations()
	drifts := reg.Drift(providers)

	if *matrix {
		printMatrix(reg)
		printProgress(reg.Unimplemented(providers))
		printDrift(drifts)
		fmt.Printf("\n数据库结构版本：代码 %d\n", store.LatestVersion())
		if *check && len(drifts) > 0 {
			os.Exit(1)
		}
		return
	}

	// 漂移是装配期问题：有漂移就别启动，否则会对外提供一个点了没反应的能力。
	if len(drifts) > 0 {
		for _, d := range drifts {
			fmt.Fprintf(os.Stderr, "✗ %s\n", d.Error())
		}
		fmt.Fprintln(os.Stderr, "装配中止：渠道声明与适配器实现不一致")
		os.Exit(1)
	}

	printStartup(reg, *addr)

	srv := &http.Server{Addr: *addr, Handler: server.New(reg).Handler()}
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("监听 %s 失败：%v", *addr, err)
	}
}

// providerDeclarations 把适配器注册表翻译成渠道守卫要的形状。
func providerDeclarations() []channel.Provider {
	adapters := adapter.All()
	out := make([]channel.Provider, 0, len(adapters))
	for _, a := range adapters {
		out = append(out, channel.Provider{
			Kind:         a.Kind(),
			Capabilities: a.Capabilities(),
		})
	}
	return out
}

// printStartup 打印一行能说明白「现在能干什么」的启动信息。
func printStartup(reg *channel.Registry, addr string) {
	ids := make([]string, 0, len(reg.All()))
	for _, c := range reg.All() {
		ids = append(ids, string(c.ID))
	}
	unimplemented := reg.Unimplemented(providerDeclarations())

	log.Printf("public2api 骨架 监听 %s", addr)
	log.Printf("渠道 %d 个：%s", len(ids), strings.Join(ids, "、"))
	log.Printf("账号类型 %d 种，已接入 %d 种",
		len(reg.Kinds()), len(reg.Kinds())-len(unimplemented))
	log.Printf("只读端点：GET /healthz、GET /v1/channels")
}

// printMatrix 打印「账号类型 × 能力」矩阵。这张表就是面板二级菜单的来源，
// 所以它同时是给人和给程序看的：矩阵里打 ● 的能力，渠道维护空间里就有对应入口。
//
// 列是**账号类型**而不是渠道：能力本来就挂在账号类型上，按渠道聚合会把差异抹平——
// 比如 grok_console 没有续期能力这件事，在渠道级视图里完全看不出来。
func printMatrix(reg *channel.Registry) {
	caps := channel.AllCapabilities()

	type col struct {
		ch   channel.ChannelID
		spec channel.KindSpec
	}
	var cols []col
	for _, c := range reg.All() {
		for _, spec := range c.Kinds {
			cols = append(cols, col{c.ID, spec})
		}
	}

	const nameW, colW = 22, 15

	fmt.Println("渠道能力矩阵（● = 该账号类型声明支持）")
	fmt.Println()

	fmt.Printf("%-*s", nameW, "能力")
	for _, c := range cols {
		fmt.Printf("  %-*s", colW, string(c.spec.Kind))
	}
	fmt.Println()
	fmt.Printf("%-*s", nameW, "")
	for _, c := range cols {
		fmt.Printf("  %-*s", colW, "("+string(c.ch)+")")
	}
	fmt.Println()

	for _, cap := range caps {
		fmt.Printf("%-*s", nameW, cap)
		for _, c := range cols {
			mark := "-"
			if c.spec.Has(cap) {
				mark = "●"
			}
			fmt.Printf("  %-*s", colW, mark)
		}
		if label := cap.Label(); label != "" {
			fmt.Printf("  → 菜单「%s」", label)
		}
		fmt.Println()
	}

	fmt.Println()
	fmt.Println("账号类型明细：")
	for _, c := range cols {
		fmt.Printf("  %-14s %-13s %s（%d 项能力）\n",
			c.ch, c.spec.Kind, c.spec.Name, len(c.spec.Capabilities))
	}
}

// printProgress 打印接入进度：哪些账号类型还没有适配器。
func printProgress(unimplemented []channel.Kind) {
	fmt.Println()
	if len(unimplemented) == 0 {
		fmt.Println("接入进度：全部账号类型已有适配器")
		return
	}
	names := make([]string, 0, len(unimplemented))
	for _, k := range unimplemented {
		names = append(names, string(k))
	}
	fmt.Printf("接入进度：尚未接入 %d 个账号类型 —— %s\n",
		len(unimplemented), strings.Join(names, "、"))
}

// printDrift 打印漂移检查结果。
func printDrift(drifts []channel.Drift) {
	fmt.Println()
	if len(drifts) == 0 {
		fmt.Println("漂移检查：通过（渠道声明与适配器实现一致）")
		return
	}
	fmt.Printf("漂移检查：发现 %d 处不一致\n", len(drifts))
	for _, d := range drifts {
		fmt.Printf("  ✗ %s\n", d.Error())
	}
}
