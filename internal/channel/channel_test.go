package channel

import (
	"strings"
	"testing"
)

// TestCatalogIsSelfConsistent 守住真实目录：改 catalog.go 时如果写出了结构不自洽的
// 描述（ID 重复、空能力、缺显示名），这里立刻红。
func TestCatalogIsSelfConsistent(t *testing.T) {
	reg, err := NewRegistry(Catalog()...)
	if err != nil {
		t.Fatalf("真实目录未通过自检：%v", err)
	}
	if got, want := len(reg.All()), 3; got != want {
		t.Fatalf("渠道数 = %d，期望 %d", got, want)
	}
	if got, want := len(reg.Kinds()), 4; got != want {
		t.Fatalf("账号类型数 = %d，期望 %d（wbp / grok_web / grok_build / grok_console）", got, want)
	}
}

// TestKindSharedByTwoChannels 钉住一条建模决定：WorkBuddy 国内与国际共用同一个
// Kind。两边是两套账号体系、但账号类型完全相同（同 OAuth 流程、同凭据结构、
// 同私有协议），差异只在端点，而端点属于渠道配置。
//
// 如果哪天有人给 KindWBP 加上了渠道后缀，这个测试会红——那时要问的不是
// 「测试怎么改」，而是「渠道抽象是不是漏了」。
func TestKindSharedByTwoChannels(t *testing.T) {
	reg, err := NewRegistry(Catalog()...)
	if err != nil {
		t.Fatal(err)
	}
	owners := reg.ChannelsOf(KindWBP)
	if len(owners) != 2 {
		t.Fatalf("KindWBP 的归属渠道数 = %d，期望 2（wbp-cn 与 wbp-global）", len(owners))
	}
	// 两个渠道的 wbp 能力集合必须完全一致——不一致就说明差异漏进了能力层。
	a, _ := reg.KindSpec(WBPChina, KindWBP)
	b, _ := reg.KindSpec(WBPGlobal, KindWBP)
	if len(a.Capabilities) != len(b.Capabilities) {
		t.Fatalf("国内 %d 项能力 vs 国际 %d 项，两边应当一致",
			len(a.Capabilities), len(b.Capabilities))
	}
	for _, cap := range a.Capabilities {
		if !b.Has(cap) {
			t.Errorf("国际版缺少国内版有的能力 %s", cap)
		}
	}
}

// TestRegistryRejectsBadCatalogs 覆盖结构自检的每条规则。
func TestRegistryRejectsBadCatalogs(t *testing.T) {
	valid := Channel{
		ID: WBPChina, Name: "国内", Sort: 1,
		Kinds: []KindSpec{{Kind: KindWBP, Name: "账号", Capabilities: []Capability{CapChat}}},
	}

	cases := []struct {
		name    string
		input   []Channel
		wantSub string
	}{
		{
			name:    "空渠道 ID",
			input:   []Channel{{Name: "x", Kinds: valid.Kinds}},
			wantSub: "渠道 ID 不能为空",
		},
		{
			name:    "渠道 ID 重复",
			input:   []Channel{valid, valid},
			wantSub: "渠道 ID 重复",
		},
		{
			name:    "缺显示名",
			input:   []Channel{{ID: Grok, Kinds: valid.Kinds}},
			wantSub: "缺少显示名",
		},
		{
			name:    "没有任何账号类型",
			input:   []Channel{{ID: Grok, Name: "x"}},
			wantSub: "没有任何账号类型",
		},
		{
			name: "账号类型没有能力",
			input: []Channel{{ID: Grok, Name: "x",
				Kinds: []KindSpec{{Kind: KindGrokWeb, Name: "web"}}}},
			wantSub: "没有声明任何能力",
		},
		{
			name: "能力重复声明",
			input: []Channel{{ID: Grok, Name: "x",
				Kinds: []KindSpec{{Kind: KindGrokWeb, Name: "web",
					Capabilities: []Capability{CapChat, CapChat}}}}},
			wantSub: "重复声明",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewRegistry(tc.input...)
			if err == nil {
				t.Fatalf("期望报错（%s），但通过了", tc.wantSub)
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("错误信息 = %q，期望包含 %q", err.Error(), tc.wantSub)
			}
		})
	}
}

// TestDriftReportsEmptyPromise 覆盖方向 ①：渠道声明了、适配器没实现。
// 这是「面板上有个按钮，点下去什么也不发生」的根因。
func TestDriftReportsEmptyPromise(t *testing.T) {
	reg, err := NewRegistry(Channel{
		ID: Grok, Name: "Grok",
		Kinds: []KindSpec{{
			Kind: KindGrokConsole, Name: "控制台",
			Capabilities: []Capability{CapChat, CapQuota},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 适配器只实现了 chat，没实现 quota。
	drifts := reg.Drift([]Provider{{Kind: KindGrokConsole, Capabilities: []Capability{CapChat}}})

	if len(drifts) != 1 {
		t.Fatalf("漂移数 = %d，期望 1：%+v", len(drifts), drifts)
	}
	if drifts[0].Cap != CapQuota {
		t.Errorf("漂移能力 = %s，期望 %s", drifts[0].Cap, CapQuota)
	}
	if !strings.Contains(drifts[0].Error(), "没有适配器实现") {
		t.Errorf("错误信息未说明方向：%s", drifts[0].Error())
	}
}

// TestDriftReportsHiddenCapability 覆盖方向 ②：适配器实现了、渠道没声明。
// 这是「功能在，但没有任何入口能到达它」。
func TestDriftReportsHiddenCapability(t *testing.T) {
	reg, err := NewRegistry(Channel{
		ID: Grok, Name: "Grok",
		Kinds: []KindSpec{{
			Kind: KindGrokConsole, Name: "控制台",
			Capabilities: []Capability{CapChat},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	drifts := reg.Drift([]Provider{{
		Kind:         KindGrokConsole,
		Capabilities: []Capability{CapChat, CapSearch}, // 多实现了一个
	}})

	if len(drifts) != 1 {
		t.Fatalf("漂移数 = %d，期望 1：%+v", len(drifts), drifts)
	}
	if drifts[0].Cap != CapSearch {
		t.Errorf("漂移能力 = %s，期望 %s", drifts[0].Cap, CapSearch)
	}
	if !strings.Contains(drifts[0].Error(), "没有声明") {
		t.Errorf("错误信息未说明方向：%s", drifts[0].Error())
	}
}

// TestUnimplementedIsNotDrift 钉住「进度」与「不一致」的分界：
// 整条链路还没接入不是漂移，否则 init 阶段会满屏噪音，把真正的漂移淹掉。
func TestUnimplementedIsNotDrift(t *testing.T) {
	reg, err := NewRegistry(Catalog()...)
	if err != nil {
		t.Fatal(err)
	}
	// 一个适配器都没有。
	if drifts := reg.Drift(nil); len(drifts) != 0 {
		t.Fatalf("零适配器不应报漂移，却报了 %d 处：%+v", len(drifts), drifts)
	}
	if got, want := len(reg.Unimplemented(nil)), 4; got != want {
		t.Fatalf("未接入数 = %d，期望 %d", got, want)
	}
}

// TestDriftReportsUnknownKind 覆盖方向 ③：适配器注册了不属于任何渠道的账号类型。
func TestDriftReportsUnknownKind(t *testing.T) {
	reg, err := NewRegistry(Catalog()...)
	if err != nil {
		t.Fatal(err)
	}
	drifts := reg.Drift([]Provider{{Kind: Kind("grok_telepathy"), Capabilities: []Capability{CapChat}}})
	if len(drifts) != 1 {
		t.Fatalf("漂移数 = %d，期望 1：%+v", len(drifts), drifts)
	}
	if !strings.Contains(drifts[0].Error(), "未在任何渠道中声明") {
		t.Errorf("错误信息未说明方向：%s", drifts[0].Error())
	}
}

// TestMenuIsDerivedFromCapabilities 钉住面板二级菜单的来源：
// 菜单是能力的派生，不是独立配置。加一个渠道不该改前端。
func TestMenuIsDerivedFromCapabilities(t *testing.T) {
	reg, err := NewRegistry(Catalog()...)
	if err != nil {
		t.Fatal(err)
	}

	cn, _ := reg.Get(WBPChina)
	menu := cn.Menu()
	wantCN := []Capability{CapQuota, CapKeepalive, CapCheckin, CapTasks}
	if len(menu) != len(wantCN) {
		t.Fatalf("国内菜单 = %v，期望 %v", menu, wantCN)
	}
	for i := range wantCN {
		if menu[i] != wantCN[i] {
			t.Fatalf("国内菜单 = %v，期望 %v", menu, wantCN)
		}
	}

	// Grok 没有签到/任务，但多一个出口——这正是「菜单由能力生成」的意义。
	gk, _ := reg.Get(Grok)
	gkMenu := gk.Menu()
	for _, cap := range gkMenu {
		if cap == CapCheckin || cap == CapTasks {
			t.Errorf("Grok 不该有 %s 菜单项", cap)
		}
	}
	if !gk.Has(CapEgress) {
		t.Error("Grok 应当有出口能力")
	}
	if cn.Has(CapEgress) {
		t.Error("WorkBuddy 不该有出口能力")
	}
}

// TestGrokConsoleHasNoRefresh 钉住一条业务约束：控制台链路的凭据没有续期机制，
// 过期只能人工重粘。它必须有失效告警与一键重粘，而不是自动续期。
//
// 这个测试的意义是：如果哪天有人「顺手」给 grok_console 加上 CapRefresh，
// 会立刻红——因为那会渲染出一个永远不会成功的自动续期按钮。
func TestGrokConsoleHasNoRefresh(t *testing.T) {
	reg, err := NewRegistry(Catalog()...)
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := reg.KindSpec(Grok, KindGrokConsole)
	if !ok {
		t.Fatal("找不到 grok_console")
	}
	if spec.Has(CapRefresh) {
		t.Error("grok_console 不应声明续期能力：它的凭据没有 refresh 机制")
	}
	// 反过来，build 有 refresh token，必须声明。
	build, _ := reg.KindSpec(Grok, KindGrokBuild)
	if !build.Has(CapRefresh) {
		t.Error("grok_build 应当声明续期能力：它有 refresh token")
	}
}
