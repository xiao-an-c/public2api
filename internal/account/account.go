// Package account 是跨渠道的统一账号模型。
//
// 一个账号 = 「某个渠道下、某种账号类型的一份凭据」，外加它的额度与健康状态。
// 渠道与账号类型来自 channel 包；凭据本身**不在这里**——它以密文存库，
// 只有适配器在发起请求前按需解密（见 internal/store 与 ADR-0002）。
package account

import (
	"time"

	"github.com/xiao-an-c/public2api/internal/channel"
)

// Status 是账号的生命周期状态。它是**单值**的，因为一个账号在任一时刻
// 只可能处于一种状态：要么能用，要么在冷却，要么被人为停用，要么凭据已失效。
type Status string

const (
	// StatusActive 可用，参与选号。
	StatusActive Status = "active"
	// StatusCooling 冷却中（限流或额度耗尽），到期自动回到 active。
	StatusCooling Status = "cooling"
	// StatusDisabled 被运维手动停用，不参与选号，也不会自动恢复。
	StatusDisabled Status = "disabled"
	// StatusInvalid 凭据已失效（被撤销、过期且无法续期）。需要人工重新接入。
	StatusInvalid Status = "invalid"
)

// Quota 是账号的额度快照。
//
// 不同渠道的额度单位不同：WorkBuddy 是积分，Grok 网页版是按时间窗的请求次数，
// 控制台是本地自管的次数。所以这里不假设单位，只保留原始值与来源——
// 单位语义由适配器解释，避免把三种计量方式硬塞进一个字段。
type Quota struct {
	// Remaining 是剩余量，单位由适配器定义。
	Remaining float64
	// Total 是总量，0 表示上游不提供（未知）。
	Total float64
	// Source 说明这个数字是怎么来的，决定它的可信度。
	Source QuotaSource
	// ResetAt 是额度恢复时间，零值表示不适用。
	ResetAt time.Time
	// FetchedAt 是快照时间，用于判断新鲜度。
	FetchedAt time.Time
}

// QuotaSource 是额度数字的来源。三态命名沿用了底子已有的语义，
// 因为「这是上游真值、还是本地估算」直接影响调度决策的可信度。
type QuotaSource string

const (
	// QuotaUnknown 尚未查询过。
	QuotaUnknown QuotaSource = ""
	// QuotaUpstream 上游直接返回的真值。
	QuotaUpstream QuotaSource = "upstream"
	// QuotaEstimated 本地按调用次数估算，上游不提供。
	QuotaEstimated QuotaSource = "estimated"
	// QuotaDefault 配置里的默认值，从未核实过。
	QuotaDefault QuotaSource = "default"
)

// Health 是账号的调度健康度。
//
// 它是**连续量**而不是开关：分数随失败下降、随时间自己恢复，
// 这样刚出过一次限流的账号只是排序靠后，而不是被完全弃用。
type Health struct {
	// Score 是调度分，基线 100，失败扣分、随时间线性回升。
	Score float64
	// ConsecutiveFailures 是连续失败次数，用于连败降权。
	ConsecutiveFailures int
	// Cooling 为真时该账号暂不参与选号。
	Cooling bool
	// CoolReason 是冷却原因，供运维在状态页一眼看到「为什么被冷却」。
	CoolReason string
	// CoolUntil 是冷却到期时间。
	CoolUntil time.Time
	// ModelCooldowns 是模型级冷却，与账号级冷却并存：
	// 一个账号可能整体可用、但某个模型还在限额。
	ModelCooldowns []ModelCooldown
}

// ModelCooldown 是单个模型的冷却记录。
//
// 它必须带到期时间：模型级冷却的意义就是「这个号还能用，只是这个模型暂时别派」，
// 没有到期时间就无法判断它什么时候恢复，这条记录会永远生效。
type ModelCooldown struct {
	Model  string
	Until  time.Time
	Reason string
}

// Account 是一个账号的完整状态（不含凭据明文）。
type Account struct {
	ID      int64
	Channel channel.ChannelID
	Kind    channel.Kind

	// UID 是账号在渠道内的稳定标识。它与 Kind 一起构成去重键的一部分。
	UID string
	// SourceKey 是凭据的指纹，用于识别「同一个账号被重复导入」。
	// 它由凭据派生，因此不含凭据本身。
	SourceKey string

	Name  string
	Email string

	Status Status
	Quota  Quota
	Health Health

	CreatedAt time.Time
	UpdatedAt time.Time
}

// Usable 报告账号此刻是否可以被选号使用。
//
// 冷却中的账号不参与选号，但**仍然是可用账号**——它只是暂时排后。
// 所以这里判的是「能不能立刻用」，不是「账号好不好」。
func (a Account) Usable(now time.Time) bool {
	if a.Status != StatusActive {
		return false
	}
	if a.Health.Cooling && now.Before(a.Health.CoolUntil) {
		return false
	}
	return true
}

// ModelCooling 报告某模型此刻是否被该账号的模型级冷却挡住。
// 已过期的记录不挡——到期即自动恢复，不需要清理任务。
func (a Account) ModelCooling(model string, now time.Time) bool {
	for _, mc := range a.Health.ModelCooldowns {
		if mc.Model == model && now.Before(mc.Until) {
			return true
		}
	}
	return false
}
