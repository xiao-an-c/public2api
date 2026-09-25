package channel

import (
	"fmt"
	"sort"
	"strings"
)

// Provider 是适配器在装配时提交的能力声明：它只说自己「能做什么」，不带实现细节。
//
// 这样 channel 包不依赖 adapter 包，依赖方向保持单向 adapter → channel。
type Provider struct {
	Kind         Kind
	Capabilities []Capability
}

// Drift 是一处「声明与实现不一致」。
//
// 漂移是本项目最想拦住的一类 bug：它不报错、不崩，只是让某个能力悄悄失效——
// 渠道声明有「签到」，但没有任何适配器实现它，于是面板上那个按钮点下去什么也不发生。
// 所以漂移必须在**装配时**就报出来，而不是等用户点到。
type Drift struct {
	Kind    Kind
	Cap     Capability
	Channel ChannelID
	Detail  string
}

func (d Drift) Error() string {
	where := string(d.Kind)
	if d.Channel != "" {
		where = fmt.Sprintf("%s/%s", d.Channel, d.Kind)
	}
	if d.Cap == "" {
		return fmt.Sprintf("漂移：%s —— %s", where, d.Detail)
	}
	return fmt.Sprintf("漂移：%s 的 %s —— %s", where, d.Cap, d.Detail)
}

// Registry 是渠道注册表。装配时构造一次，之后只读。
type Registry struct {
	byID   map[ChannelID]Channel
	byKind map[Kind][]ChannelID // 一个 Kind 可以被多个渠道共用
	order  []Channel
}

// NewRegistry 校验并构造注册表。
//
// 它只检查**结构自洽**（标识唯一、描述完整），不检查实现是否存在——
// 后者是 Drift 的职责，因为只有装配层才知道有哪些适配器。
//
// 注意这里**不要求** Kind 全局唯一：WorkBuddy 国内与国际是两套账号体系，但账号
// 类型完全相同（同一套 OAuth 流程、同一套凭据结构、同一个私有协议），差异只在端点，
// 而端点属于渠道配置。所以 KindWBP 同时属于两个渠道是正确的建模，不是冲突。
// 「这个账号属于哪个渠道」由 accounts.channel 列单值回答，不由 Kind 反推。
func NewRegistry(channels ...Channel) (*Registry, error) {
	r := &Registry{byID: make(map[ChannelID]Channel), byKind: make(map[Kind][]ChannelID)}
	for _, c := range channels {
		if strings.TrimSpace(string(c.ID)) == "" {
			return nil, fmt.Errorf("渠道 ID 不能为空")
		}
		if _, dup := r.byID[c.ID]; dup {
			return nil, fmt.Errorf("渠道 ID 重复：%s", c.ID)
		}
		if strings.TrimSpace(c.Name) == "" {
			return nil, fmt.Errorf("渠道 %s 缺少显示名", c.ID)
		}
		if len(c.Kinds) == 0 {
			return nil, fmt.Errorf("渠道 %s 没有任何账号类型", c.ID)
		}
		seenKinds := make(map[Kind]struct{}, len(c.Kinds))
		for _, spec := range c.Kinds {
			if strings.TrimSpace(string(spec.Kind)) == "" {
				return nil, fmt.Errorf("渠道 %s 存在空的账号类型", c.ID)
			}
			// 同一个 Kind 在一个渠道内不能出现两次，否则能力并集会悄悄重复计入。
			if _, dup := seenKinds[spec.Kind]; dup {
				return nil, fmt.Errorf("渠道 %s 重复声明账号类型 %s", c.ID, spec.Kind)
			}
			seenKinds[spec.Kind] = struct{}{}
			if len(spec.Capabilities) == 0 {
				return nil, fmt.Errorf("%s/%s 没有声明任何能力", c.ID, spec.Kind)
			}
			seen := make(map[Capability]struct{}, len(spec.Capabilities))
			for _, cap := range spec.Capabilities {
				if _, dup := seen[cap]; dup {
					return nil, fmt.Errorf("%s/%s 的能力 %s 重复声明", c.ID, spec.Kind, cap)
				}
				seen[cap] = struct{}{}
			}
		}
		for _, spec := range c.Kinds {
			r.byKind[spec.Kind] = append(r.byKind[spec.Kind], c.ID)
		}
		r.byID[c.ID] = c
		r.order = append(r.order, c)
	}
	sort.SliceStable(r.order, func(i, j int) bool { return r.order[i].Sort < r.order[j].Sort })
	return r, nil
}

// All 按展示顺序返回全部渠道。
func (r *Registry) All() []Channel { return append([]Channel(nil), r.order...) }

// Get 按标识取渠道。
func (r *Registry) Get(id ChannelID) (Channel, bool) {
	c, ok := r.byID[id]
	return c, ok
}

// ChannelsOf 返回所有拥有该账号类型的渠道（可能不止一个）。
func (r *Registry) ChannelsOf(k Kind) []ChannelID {
	return append([]ChannelID(nil), r.byKind[k]...)
}

// Partitioned 返回所有声明了上游分区的渠道，按展示顺序。
//
// 服务层遍历「域」时用它，而不是写死 cn/global——加一个带分区的渠道，
// 分域统计就自动多一项，不需要改任何调用点。
func (r *Registry) Partitioned() []Channel {
	var out []Channel
	for _, c := range r.order {
		if c.Partition != "" {
			out = append(out, c)
		}
	}
	return out
}

// Kinds 按渠道展示顺序返回去重后的全部账号类型。
func (r *Registry) Kinds() []Kind {
	seen := make(map[Kind]struct{})
	var out []Kind
	for _, c := range r.order {
		for _, spec := range c.Kinds {
			if _, dup := seen[spec.Kind]; dup {
				continue
			}
			seen[spec.Kind] = struct{}{}
			out = append(out, spec.Kind)
		}
	}
	return out
}

// KindSpec 按账号类型取出它在某个渠道下的描述。
func (r *Registry) KindSpec(id ChannelID, k Kind) (KindSpec, bool) {
	c, ok := r.byID[id]
	if !ok {
		return KindSpec{}, false
	}
	return c.Kind(k)
}

// Drift 双向核对「渠道声明」与「适配器实现」，返回全部不一致处。
//
// 两个方向都要查，因为两种漂移都会在运行期咬人：
//
//	① 声明了、没实现 → 空头承诺。面板会渲染出一个点了没反应的按钮。
//	② 实现了、没声明 → 隐性能力。功能在，但没有任何入口能到达它。
//
// 由于一个 Kind 可能被多个渠道共用，实现只核对一次，但要在**每个**拥有它的渠道下
// 都满足声明。
//
// 返回空切片表示无漂移。调用方在装配期用它决定是否启动失败。
func (r *Registry) Drift(providers []Provider) []Drift {
	impl := make(map[Kind]map[Capability]struct{}, len(providers))
	for _, p := range providers {
		set, ok := impl[p.Kind]
		if !ok {
			set = make(map[Capability]struct{}, len(p.Capabilities))
			impl[p.Kind] = set
		}
		for _, cap := range p.Capabilities {
			set[cap] = struct{}{}
		}
	}

	var out []Drift
	for _, c := range r.order {
		for _, spec := range c.Kinds {
			have, registered := impl[spec.Kind]
			if !registered {
				// 整条链路还没接入。这是**进度**不是**漂移**——由 Unimplemented
				// 报告。把两者混在一起会让 init 阶段满屏噪音，掩盖真正的漂移。
				continue
			}

			// ① 渠道声明了，适配器没实现。
			for _, cap := range spec.Capabilities {
				if _, ok := have[cap]; !ok {
					out = append(out, Drift{
						Kind: spec.Kind, Cap: cap, Channel: c.ID,
						Detail: "渠道声明了这个能力，但没有适配器实现它",
					})
				}
			}
			// ② 适配器实现了，渠道没声明。
			for cap := range have {
				if !spec.Has(cap) {
					out = append(out, Drift{
						Kind: spec.Kind, Cap: cap, Channel: c.ID,
						Detail: "适配器实现了这个能力，但渠道没有声明它",
					})
				}
			}
		}
	}
	// ③ 适配器注册了一个不属于任何渠道的账号类型。
	for _, p := range providers {
		if len(r.byKind[p.Kind]) == 0 {
			out = append(out, Drift{
				Kind:   p.Kind,
				Detail: "适配器注册了未在任何渠道中声明的账号类型",
			})
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Channel != out[j].Channel {
			return out[i].Channel < out[j].Channel
		}
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Cap < out[j].Cap
	})
	return out
}

// Unimplemented 返回还没有任何适配器实现的账号类型。
//
// 它报告的是**接入进度**，与 Drift 报告的**不一致**是两回事：init 阶段
// 全部 kind 都未接入是预期状态，不是错误。
func (r *Registry) Unimplemented(providers []Provider) []Kind {
	have := make(map[Kind]struct{}, len(providers))
	for _, p := range providers {
		have[p.Kind] = struct{}{}
	}
	var out []Kind
	for _, k := range r.Kinds() {
		if _, ok := have[k]; !ok {
			out = append(out, k)
		}
	}
	return out
}
