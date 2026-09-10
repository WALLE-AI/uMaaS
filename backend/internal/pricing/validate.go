package pricing

import "fmt"

// 校验的位置是本节最重要的判断：**全部前移到 admin 的提交/发布写路径**，
// 运行时只做兜底（§3.6-④）。
//
// 理由很实际：既然价目由人手工填，"填错"就是常态而非异常。而手填价格最贵的
// 一类错误不是"填了个略高略低的数"，是**单位错位**——把"每千 token"当成
// "每百万 token"填，差 1000 倍。这类错误一旦上线，几分钟内就是几个数量级的
// 资损或白送，**且用户不会来报"你们收得太便宜了"**。

// Severity 区分"必须拦住"与"提示一下"。
//
// 这个区分本身是有代价的判断：把"输出价 < 输入价"做成阻断会挡住少数真实模型，
// 做成提示又会被人无脑点过。选提示，是因为它的误报率远高于漏报危害。
type Severity string

const (
	SeverityError Severity = "error" // 阻断提交
	SeverityWarn  Severity = "warn"  // 提示，需二次确认
)

type Issue struct {
	Severity Severity `json:"severity"`
	Code     string   `json:"code"`
	Message  string   `json:"message"`
	Field    string   `json:"field,omitempty"`
}

// ValidateInput 是提交校验的上下文。
type ValidateInput struct {
	Rates *Rates
	// Capabilities 是模型声明的能力（models.capabilities）。
	Capabilities []string
	// Hosting 为 "self" 时必须有售价：自建模型的售价机制与云端完全相同，
	// 不同的只是成本侧（§3.6-⑤）。没有售价的自建模型每一个请求都是白送，
	// 且**事后无法追补**——价目按 received_at 解析，新版本不覆盖历史请求。
	Hosting string
	// ReferencePerMillionNano 是上游标价（有则比对数量级）。缺省为 0 表示没有参照。
	ReferenceInputPerMillionNano  int64
	ReferenceOutputPerMillionNano int64
	// Note 在"近 7 日有真实调用"时是必填（§3.6-⑥）。
	Note            string
	RecentCallCount int64
}

// 数量级哨兵。这两个边界不是精算出来的，是用来抓"错了 1000 倍"的：
// $0.0001 / 1M 以下与 $1000 / 1M 以上都不是真实存在的 token 价。
const (
	implausiblyCheapNano = 100_000           // $0.0001 / 1M
	implausiblyDearNano  = 1_000_000_000_000 // $1000 / 1M
)

// Validate 返回全部问题。**调用方要在有 error 时拒绝写入**，
// 有 warn 时要求二次确认。
func Validate(in ValidateInput) []Issue {
	var issues []Issue
	rs := in.Rates
	if rs == nil {
		return []Issue{{SeverityError, "rates_missing", "price version has no rates", "rates"}}
	}

	inputRate, inputOK := rs.PerMillionNano(UnitInput)
	outputRate, outputOK := rs.PerMillionNano(UnitOutput)

	if !inputOK {
		issues = append(issues, Issue{SeverityError, "input_rate_missing",
			"每个可调用模型都必须有 input 费率", "rates.input"})
	}
	if !outputOK {
		issues = append(issues, Issue{SeverityError, "output_rate_missing",
			"每个可调用模型都必须有 output 费率", "rates.output"})
	}

	// 计价单元与埋点匹配：模型声明支持缓存，价目却没有 cache_read 费率，
	// 结果是 request_logs 记了 token 但算不出钱（§3.2）。
	caps := map[string]bool{}
	for _, c := range in.Capabilities {
		caps[c] = true
	}
	if (caps["cache"] || caps["prompt_caching"]) && !rs.Has(UnitCacheRead) {
		issues = append(issues, Issue{SeverityError, "cache_rate_missing",
			"模型声明了缓存能力，价目里必须有 cache_read 费率", "rates.cache_read"})
	}
	if (caps["reasoning"] || caps["thinking"]) && !rs.Has(UnitReasoning) {
		// 与 output 同价也要**显式写出** {"same_as":"output"}，
		// 否则某个厂商改成单列计价时，改动点找不到（§3.2）。
		issues = append(issues, Issue{SeverityWarn, "reasoning_rate_missing",
			`模型声明了推理能力但没有 reasoning 费率；与 output 同价时也要显式写 {"same_as":"output"}`,
			"rates.reasoning"})
	}

	if inputOK && outputOK && outputRate < inputRate {
		issues = append(issues, Issue{SeverityWarn, "output_cheaper_than_input",
			"输出价低于输入价。绝大多数模型不是这样，请确认没有把两个数填反", "rates.output"})
	}

	for _, unit := range []struct {
		u     Unit
		field string
	}{{UnitInput, "rates.input"}, {UnitOutput, "rates.output"}, {UnitCacheRead, "rates.cache_read"}} {
		nano, ok := rs.PerMillionNano(unit.u)
		if !ok || nano == 0 {
			continue // 0 是合法的"免费"，未声明由上面的规则管
		}
		if nano < implausiblyCheapNano || nano > implausiblyDearNano {
			issues = append(issues, Issue{SeverityError, "rate_magnitude_implausible",
				fmt.Sprintf("%s 的单价 %s 超出合理数量级，极可能是单位填错（把每千 token 当成每百万 token）",
					unit.u, formatUSDPerMillion(nano)), unit.field})
		}
	}

	// 与上游标价比对：偏离一个数量级以上基本只能是单位错。
	if in.ReferenceInputPerMillionNano > 0 && inputOK {
		if magnitudeOff(inputRate, in.ReferenceInputPerMillionNano) {
			issues = append(issues, Issue{SeverityError, "rate_deviates_from_upstream",
				fmt.Sprintf("input 单价 %s 与上游标价 %s 相差一个数量级以上",
					formatUSDPerMillion(inputRate), formatUSDPerMillion(in.ReferenceInputPerMillionNano)),
				"rates.input"})
		}
	}
	if in.ReferenceOutputPerMillionNano > 0 && outputOK {
		if magnitudeOff(outputRate, in.ReferenceOutputPerMillionNano) {
			issues = append(issues, Issue{SeverityError, "rate_deviates_from_upstream",
				fmt.Sprintf("output 单价 %s 与上游标价 %s 相差一个数量级以上",
					formatUSDPerMillion(outputRate), formatUSDPerMillion(in.ReferenceOutputPerMillionNano)),
				"rates.output"})
		}
	}

	if in.Hosting == "self" && (!inputOK || !outputOK) {
		issues = append(issues, Issue{SeverityError, "self_hosted_needs_price",
			"自建模型必须有售价：售价机制与云端完全相同，无价可计的请求是白送且事后无法追补",
			"rates"})
	}

	// 已有真实调用量的模型改价，强制填 note（§3.6-⑥）。
	// 事后追查"这次改价是谁、为什么"时，note 的价值远高于一条只有时间戳的审计记录。
	if in.RecentCallCount > 0 && in.Note == "" {
		issues = append(issues, Issue{SeverityError, "note_required",
			fmt.Sprintf("该模型近 7 日有 %d 次调用，改价必须填写 note", in.RecentCallCount), "note"})
	}

	return issues
}

// HasErrors 报告是否存在阻断级问题。
func HasErrors(issues []Issue) bool {
	for _, i := range issues {
		if i.Severity == SeverityError {
			return true
		}
	}
	return false
}

// magnitudeOff 判断两个单价是否相差一个数量级以上。
func magnitudeOff(actual, reference int64) bool {
	if reference <= 0 {
		return false
	}
	return actual >= reference*10 || actual*10 <= reference
}

func formatUSDPerMillion(nano int64) string {
	return fmt.Sprintf("$%.6f / 1M", PerMillionUSD(nano))
}
