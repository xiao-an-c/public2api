package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/xiao-an-c/public2api/internal/channel"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	reg, err := channel.NewRegistry(channel.Catalog()...)
	if err != nil {
		t.Fatalf("构造渠道注册表：%v", err)
	}
	ts := httptest.NewServer(New(reg).Handler())
	t.Cleanup(ts.Close)
	return ts
}

func getJSON(t *testing.T, url string, into any) int {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s：%v", url, err)
	}
	defer func() { _ = resp.Body.Close() }()
	if into != nil && resp.StatusCode == http.StatusOK {
		if err := json.NewDecoder(resp.Body).Decode(into); err != nil {
			t.Fatalf("解析 %s 响应：%v", url, err)
		}
	}
	return resp.StatusCode
}

func TestHealthz(t *testing.T) {
	ts := newTestServer(t)

	var body map[string]any
	if code := getJSON(t, ts.URL+"/healthz", &body); code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", code)
	}
	if body["ok"] != true {
		t.Errorf("ok = %v，期望 true", body["ok"])
	}
}

func TestRootListsEndpoints(t *testing.T) {
	ts := newTestServer(t)

	var body struct {
		Status    string   `json:"status"`
		Endpoints []string `json:"endpoints"`
	}
	if code := getJSON(t, ts.URL+"/", &body); code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", code)
	}
	if body.Status != "skeleton" {
		t.Errorf("status = %q，期望 skeleton", body.Status)
	}
	if len(body.Endpoints) == 0 {
		t.Error("根端点应当列出可用端点")
	}
}

// TestChannelsExposesCatalog 钉住对外契约：渠道目录的三个渠道、
// 以及每个渠道的账号类型都在响应里。
func TestChannelsExposesCatalog(t *testing.T) {
	ts := newTestServer(t)

	var body struct {
		Channels []channelDTO `json:"channels"`
	}
	if code := getJSON(t, ts.URL+"/v1/channels", &body); code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", code)
	}
	if len(body.Channels) != 3 {
		t.Fatalf("渠道数 = %d，期望 3", len(body.Channels))
	}

	byID := map[string]channelDTO{}
	for _, c := range body.Channels {
		byID[c.ID] = c
	}

	cn, ok := byID["wbp-cn"]
	if !ok {
		t.Fatal("缺少 wbp-cn")
	}
	if cn.Name != "WorkBuddy 国内" || cn.UpstreamHost != "codebuddy.cn" {
		t.Errorf("wbp-cn 的展示信息不对：%+v", cn)
	}
	if len(cn.Kinds) != 1 || cn.Kinds[0].Kind != "wbp" {
		t.Errorf("wbp-cn 的账号类型不对：%+v", cn.Kinds)
	}

	grok, ok := byID["grok"]
	if !ok {
		t.Fatal("缺少 grok")
	}
	if len(grok.Kinds) != 3 {
		t.Fatalf("grok 的账号类型数 = %d，期望 3（web / build / console）", len(grok.Kinds))
	}
}

// TestChannelsMenuIsDerived 钉住「菜单由能力派生」这件事在 API 上可见：
// WorkBuddy 有签到与任务、没有出口；Grok 有出口、没有签到与任务。
//
// 这条断言的价值在于：如果哪天菜单变成前端配置，它一定会红。
func TestChannelsMenuIsDerived(t *testing.T) {
	ts := newTestServer(t)

	var body struct {
		Channels []channelDTO `json:"channels"`
	}
	getJSON(t, ts.URL+"/v1/channels", &body)

	menuOf := func(id string) map[string]string {
		for _, c := range body.Channels {
			if c.ID == id {
				out := map[string]string{}
				for _, m := range c.Menu {
					out[m.Capability] = m.Label
				}
				return out
			}
		}
		t.Fatalf("找不到渠道 %s", id)
		return nil
	}

	cn := menuOf("wbp-cn")
	if cn["ops.checkin"] != "签到" {
		t.Errorf("wbp-cn 应当有签到菜单，实际：%v", cn)
	}
	if cn["ops.tasks"] != "任务" {
		t.Errorf("wbp-cn 应当有任务菜单，实际：%v", cn)
	}
	if _, has := cn["ops.egress"]; has {
		t.Errorf("wbp-cn 不该有出口菜单，实际：%v", cn)
	}

	grok := menuOf("grok")
	if grok["ops.egress"] != "出口" {
		t.Errorf("grok 应当有出口菜单，实际：%v", grok)
	}
	if _, has := grok["ops.checkin"]; has {
		t.Errorf("grok 不该有签到菜单，实际：%v", grok)
	}
}

func TestUnknownPathIs404(t *testing.T) {
	ts := newTestServer(t)
	if code := getJSON(t, ts.URL+"/v1/chat/completions", nil); code != http.StatusNotFound {
		t.Errorf("未实现的对话端点状态码 = %d，期望 404（骨架阶段还没有适配器）", code)
	}
}
