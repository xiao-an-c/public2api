package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/xiao-an-c/public2api/internal/channel"
	"github.com/xiao-an-c/public2api/internal/pool"
	"github.com/xiao-an-c/public2api/internal/upstream"
)

func kindSpec(k channel.Kind) channel.KindSpec {
	return channel.KindSpec{
		Kind:         k,
		Name:         "账号",
		Capabilities: []channel.Capability{channel.CapChat},
	}
}

// getJSONInto 只要求 body 可解析，**不检查状态码**。
//
// 因为 healthz 在「无账号可服务」时返回 503 —— 那是底子的正确语义（探活与
// 可服务性同口径），不是错误。空池的测试正是这种情况。状态码断言留给各自需要的测试。
func getJSONInto(t *testing.T, h *Handler, path string, into any) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", path, nil))
	if err := json.Unmarshal(rec.Body.Bytes(), into); err != nil {
		t.Fatalf("解析 %s 响应失败（状态码 %d）：%v，body=%s", path, rec.Code, err, rec.Body)
	}
}

// TestRealmKeysAreChannelDriven 钉住这次改造的核心：healthz 的 realm_servable
// 与 status 的 realm_totals，键**由渠道注册表决定**，不再是代码里写死的 cn/global。
//
// 做法是注入一个带第三个分区（eu）的注册表——如果键还是写死的，eu 不会出现。
// 这条断言的价值在于：将来加带分区的渠道时，它保证统计自动跟上；
// 而如果有人把遍历改回硬编码，它会红。
func TestRealmKeysAreChannelDriven(t *testing.T) {
	reg, err := channel.NewRegistry(
		channel.Channel{ID: channel.WBPChina, Name: "国内", Partition: "cn",
			Kinds: []channel.KindSpec{kindSpec(channel.KindWBP)}},
		channel.Channel{ID: channel.WBPGlobal, Name: "国际", Partition: "global",
			Kinds: []channel.KindSpec{kindSpec(channel.KindWBP)}},
		channel.Channel{ID: "wbp-eu", Name: "欧洲", Partition: "eu",
			Kinds: []channel.KindSpec{kindSpec("wbp_eu")}},
	)
	if err != nil {
		t.Fatalf("构造注册表：%v", err)
	}

	h := NewHandler(Config{Pool: pool.New(""), Upstream: upstream.New(), Channels: reg})

	var hz struct {
		RealmServable map[string]bool `json:"realm_servable"`
	}
	getJSONInto(t, h, "/healthz", &hz)
	for _, want := range []string{"cn", "global", "eu"} {
		if _, ok := hz.RealmServable[want]; !ok {
			t.Errorf("realm_servable 缺少分区 %q——键应当来自渠道注册表：%v", want, hz.RealmServable)
		}
	}
	if len(hz.RealmServable) != 3 {
		t.Errorf("realm_servable 有 %d 个键，期望 3：%v", len(hz.RealmServable), hz.RealmServable)
	}

	var st struct {
		RealmTotals map[string]map[string]int `json:"realm_totals"`
	}
	getJSONInto(t, h, "/status", &st)
	if len(st.RealmTotals) != 3 {
		t.Errorf("realm_totals 有 %d 个键，期望 3：%v", len(st.RealmTotals), st.RealmTotals)
	}
	for _, want := range []string{"cn", "global", "eu"} {
		if _, ok := st.RealmTotals[want]; !ok {
			t.Errorf("realm_totals 缺少分区 %q：%v", want, st.RealmTotals)
		}
	}
}

// TestBuiltinCatalogKeepsRealmContract 守兼容性：不注入注册表时回退内置目录，
// 键仍是 cn/global——对外契约与改造前完全一致。
//
// 同时验证 Partition 空串的语义：grok 没有分区，不该出现在分域统计里。
func TestBuiltinCatalogKeepsRealmContract(t *testing.T) {
	h := NewHandler(Config{Pool: pool.New(""), Upstream: upstream.New()})

	var hz struct {
		RealmServable map[string]bool `json:"realm_servable"`
	}
	getJSONInto(t, h, "/healthz", &hz)

	if len(hz.RealmServable) != 2 {
		t.Fatalf("内置目录下 realm_servable 应有 2 个键（cn/global），实际：%v", hz.RealmServable)
	}
	for _, want := range []string{"cn", "global"} {
		if _, ok := hz.RealmServable[want]; !ok {
			t.Errorf("缺少分区 %q：%v", want, hz.RealmServable)
		}
	}
	if _, ok := hz.RealmServable["grok"]; ok {
		t.Error("grok 没有声明 Partition，不该出现在 realm_servable 里")
	}
}

// TestChannelRegistryIsInjected 确认注入的注册表真的被用上了（而不是被忽略后
// 悄悄回退到内置目录）——上面那个测试只有配上这条才有说服力。
func TestChannelRegistryIsInjected(t *testing.T) {
	reg, err := channel.NewRegistry(
		channel.Channel{ID: "only-one", Name: "唯一渠道", Partition: "solo",
			Kinds: []channel.KindSpec{kindSpec("solo_kind")}},
	)
	if err != nil {
		t.Fatal(err)
	}
	h := NewHandler(Config{Pool: pool.New(""), Upstream: upstream.New(), Channels: reg})

	var hz struct {
		RealmServable map[string]bool `json:"realm_servable"`
	}
	getJSONInto(t, h, "/healthz", &hz)

	if len(hz.RealmServable) != 1 {
		t.Fatalf("注入的注册表只有 1 个分区，实际得到 %v（说明注入被忽略了）", hz.RealmServable)
	}
	if _, ok := hz.RealmServable["solo"]; !ok {
		t.Errorf("缺少注入的分区 solo：%v", hz.RealmServable)
	}
	if _, ok := hz.RealmServable["cn"]; ok {
		t.Error("出现了内置目录的 cn——说明回退到了内置目录而不是用注入的注册表")
	}
}
