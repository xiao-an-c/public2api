// 本文件是外部测试包（channel_test）：漂移守卫要同时引用渠道目录与适配器注册表，
// 放在 channel 包内会造成 adapter → channel → adapter 的循环。
package channel_test

import (
	"testing"

	"github.com/xiao-an-c/public2api/internal/adapter"
	"github.com/xiao-an-c/public2api/internal/channel"
)

// TestNoDrift 是漂移守卫的常驻关卡。
//
// 它拿**真实的**渠道目录与**真实的**适配器注册表核对，所以每接入一条链路，
// 这个测试就自动开始守它——不需要有人记得去敲某条命令。
// 目前注册表为空，测试通过是预期状态（未接入不是漂移）。
func TestNoDrift(t *testing.T) {
	reg, err := channel.NewRegistry(channel.Catalog()...)
	if err != nil {
		t.Fatalf("渠道目录自检失败：%v", err)
	}

	adapters := adapter.All()
	providers := make([]channel.Provider, 0, len(adapters))
	for _, a := range adapters {
		providers = append(providers, channel.Provider{
			Kind:         a.Kind(),
			Capabilities: a.Capabilities(),
		})
	}

	for _, d := range reg.Drift(providers) {
		t.Error(d.Error())
	}
}

// TestAdapterDeclarationsMatchInterfaces 守第二条漂移：
// 适配器**声明**的能力 vs 它**实际实现**的能力接口。
//
// 覆盖面目前只有四项（chat / quota / refresh / credential）——其余能力还没有
// 专属接口，只能靠声明自觉。等接口补齐，这个测试自动扩大覆盖面，
// 直到 Capabilities() 可以被删掉。
func TestAdapterDeclarationsMatchInterfaces(t *testing.T) {
	detectable := make(map[channel.Capability]bool)
	for _, c := range adapter.Detected() {
		detectable[c] = true
	}

	for _, a := range adapter.All() {
		implemented := make(map[channel.Capability]bool)
		for _, c := range adapter.Detect(a) {
			implemented[c] = true
		}
		for _, declared := range a.Capabilities() {
			if !detectable[declared] {
				continue // 还没有专属接口，跳过
			}
			if !implemented[declared] {
				t.Errorf("%s 声明了 %s，但没有实现对应的能力接口",
					a.Kind(), declared)
			}
		}
	}
}
