package panel

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"github.com/xiao-an-c/public2api/internal/auth"
	"github.com/xiao-an-c/public2api/internal/pool"
	"github.com/xiao-an-c/public2api/internal/upstream"
)

func TestChannelsEndpointExposesChannelMenus(t *testing.T) {
	p := New(Config{APIKey: "test-key"})
	req := httptest.NewRequest("GET", "/panel/api/channels", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body)
	}
	var body struct {
		Channels []struct {
			ID   string `json:"id"`
			Menu []struct {
				Capability string `json:"capability"`
				Label      string `json:"label"`
			} `json:"menu"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Channels) != 3 {
		t.Fatalf("渠道数=%d，期望 3", len(body.Channels))
	}
	menu := func(id string) map[string]string {
		for _, c := range body.Channels {
			if c.ID == id {
				out := map[string]string{}
				for _, item := range c.Menu {
					out[item.Capability] = item.Label
				}
				return out
			}
		}
		t.Fatalf("缺少渠道 %s", id)
		return nil
	}
	cn, global := menu("wbp-cn"), menu("wbp-global")
	if cn["ops.checkin"] != "签到" || cn["ops.tasks"] != "任务" {
		t.Fatalf("国内菜单缺签到/任务：%v", cn)
	}
	if _, ok := global["ops.checkin"]; ok {
		t.Fatalf("国际菜单不应有签到：%v", global)
	}
	if _, ok := global["ops.tasks"]; ok {
		t.Fatalf("国际菜单不应有任务：%v", global)
	}
	if global["ops.keepalive"] != "保活" {
		t.Fatalf("国际菜单应保留保活：%v", global)
	}
}

func TestGlobalAccountCheckinIsCapabilityGated(t *testing.T) {
	p := pool.New("")
	p.Add(&auth.Auth{UID: "global-1", Domain: "www.workbuddy.ai", RefreshToken: "rt"})
	panel := New(Config{APIKey: "test-key", Pool: p, Upstream: upstream.New()})
	req := httptest.NewRequest("POST", "/panel/api/accounts/global-1/checkin", nil)
	req.Header.Set("Authorization", "Bearer test-key")
	rec := httptest.NewRecorder()
	panel.ServeHTTP(rec, req)
	if rec.Code != 501 {
		t.Fatalf("国际账号签到 status=%d body=%s，应该在能力门控处拒绝", rec.Code, rec.Body)
	}
}
