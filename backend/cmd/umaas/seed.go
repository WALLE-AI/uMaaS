package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/WALLE-AI/uMaaS/backend/db/seed"
	"github.com/WALLE-AI/uMaaS/backend/internal/catalogadmin"
	"github.com/WALLE-AI/uMaaS/backend/internal/config"
	"github.com/WALLE-AI/uMaaS/backend/internal/store"
	"github.com/WALLE-AI/uMaaS/backend/internal/store/postgres"
)

// runSeedCommand 实现 `umaas seed catalog`。
//
// # 为什么种子数据走 admin 的写入路径，而不是一段 INSERT
//
// 因为那条写入路径上挂着 M1a 的全部校验（价目完整性、单位数量级、
// 发布前的价目检查）。**用 INSERT 灌数据等于绕开自己刚立的规矩**——
// 而绕开一次之后，"临时先绕一下"就会变成常态。
// 副作用是：种子跑通本身就证明了写入路径能用。
func runSeedCommand(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: umaas seed catalog [--config <path>] [--file <path>]")
	}
	verb, rest := args[0], args[1:]
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config file")
	file := fs.String("file", "", "seed file (defaults to the embedded db/seed/catalog.json)")
	_ = fs.Parse(rest)

	if verb != "catalog" {
		return fmt.Errorf("unknown seed subcommand %q; usage: umaas seed catalog", verb)
	}

	raw := seed.Catalog
	if *file != "" {
		b, err := os.ReadFile(*file)
		if err != nil {
			return fmt.Errorf("read seed file: %w", err)
		}
		raw = b
	}
	var data seedData
	if err := json.Unmarshal(raw, &data); err != nil {
		return fmt.Errorf("parse seed file: %w", err)
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return err
	}
	if cfg.Env == "prod" {
		// 生产目录由 admin 逐条录入并留痕。一条命令灌进去的模型，
		// audit_logs 里的 actor 会是 "seed"，而那是无法向审计解释的。
		return errors.New("refusing to seed in prod; the catalog is entered through admin")
	}

	ctx := context.Background()
	db, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()

	svc := catalogadmin.NewService(postgres.NewCatalogAdminRepo(db), postgres.NewAuditRepo(db))
	actor := catalogadmin.Actor{Label: "seed", RequestID: "seed"}
	return data.apply(ctx, db, svc, actor)
}

// ── 种子文件的形状 ──────────────────────────────────────────────

type seedData struct {
	Providers []struct {
		Slug    string `json:"slug"`
		Name    string `json:"name"`
		Kind    string `json:"kind"`
		LogoURL string `json:"logo_url"`
	} `json:"providers"`
	Models []struct {
		Provider            string          `json:"provider"`
		Slug                string          `json:"slug"`
		Name                string          `json:"name"`
		Description         string          `json:"description"`
		Modalities          []string        `json:"modalities"`
		Capabilities        []string        `json:"capabilities"`
		ContextLength       *int32          `json:"context_length"`
		MaxOutputTokens     *int32          `json:"max_output_tokens"`
		Architecture        *string         `json:"architecture"`
		InputFormats        []string        `json:"input_formats"`
		OutputFormats       []string        `json:"output_formats"`
		SupportedParameters []string        `json:"supported_parameters"`
		ZeroDataRetention   bool            `json:"zero_data_retention"`
		OpenWeights         bool            `json:"open_weights"`
		Hosting             string          `json:"hosting"`
		Featured            bool            `json:"featured"`
		FeaturedRank        *int32          `json:"featured_rank"`
		QualityScore        *float64        `json:"quality_score"`
		ReleasedAt          string          `json:"released_at"`
		Status              string          `json:"status"`
		Rates               json.RawMessage `json:"rates"`
		Note                string          `json:"note"`
	} `json:"models"`
	Benchmarks []struct {
		Slug          string          `json:"slug"`
		Name          string          `json:"name"`
		Category      string          `json:"category"`
		Description   string          `json:"description"`
		Methodology   string          `json:"methodology"`
		Configuration json.RawMessage `json:"configuration"`
		RunCount      int32           `json:"run_count"`
		LastRunAt     string          `json:"last_run_at"`
		Results       []struct {
			Model       string  `json:"model"`
			Score       float64 `json:"score"`
			Percentile  float64 `json:"percentile"`
			CostNano    int64   `json:"cost_nano"`
			DurationMs  float64 `json:"duration_ms"`
			SampleCount int32   `json:"sample_count"`
		} `json:"results"`
	} `json:"benchmarks"`
	FAQ []struct {
		Model string `json:"model"`
		Items []struct {
			Question string `json:"question"`
			Answer   string `json:"answer"`
		} `json:"items"`
	} `json:"faq"`
	Docs []struct {
		Slug          string `json:"slug"`
		Group         string `json:"group"`
		GroupPosition int32  `json:"group_position"`
		Position      int32  `json:"position"`
		Title         string `json:"title"`
		Description   string `json:"description"`
		Badge         string `json:"badge"`
		Body          string `json:"body"`
	} `json:"docs"`
}

func (d seedData) apply(ctx context.Context, db *store.DB, svc *catalogadmin.Service, actor catalogadmin.Actor) error {
	providers := map[string]catalogadmin.ProviderInput{}
	for _, p := range d.Providers {
		providers[p.Slug] = catalogadmin.ProviderInput{
			Slug: p.Slug, Name: p.Name, Kind: p.Kind, LogoURL: p.LogoURL,
		}
	}

	seeder := postgres.NewSeedRepo(db)
	modelIDs := map[string]int64{}

	for _, m := range d.Models {
		provider, ok := providers[m.Provider]
		if !ok {
			return fmt.Errorf("model %s/%s references unknown provider", m.Provider, m.Slug)
		}
		released, err := parseSeedTime(m.ReleasedAt)
		if err != nil {
			return err
		}
		created, err := svc.UpsertModel(ctx, actor, provider, catalogadmin.ModelInput{
			Slug: m.Slug, DisplayName: m.Name, Description: m.Description,
			LogoURL:    provider.LogoURL,
			Modalities: m.Modalities, Capabilities: m.Capabilities,
			ContextLength: m.ContextLength, MaxOutputTokens: m.MaxOutputTokens,
			Architecture: m.Architecture, InputFormats: m.InputFormats,
			OutputFormats: m.OutputFormats, SupportedParameters: m.SupportedParameters,
			ZeroDataRetention: m.ZeroDataRetention, OpenWeights: m.OpenWeights,
			Hosting: m.Hosting, ReleasedAt: released,
		})
		if err != nil {
			return fmt.Errorf("seed model %s/%s: %w", m.Provider, m.Slug, err)
		}
		modelIDs[m.Provider+"/"+m.Slug] = created.ID

		if err := seeder.SetPresentation(ctx, created.ID, m.Featured, m.FeaturedRank, m.QualityScore); err != nil {
			return err
		}

		if len(m.Rates) > 0 && string(m.Rates) != "null" {
			if _, _, err := svc.PublishPrice(ctx, actor, catalogadmin.PriceProposal{
				ModelID: created.ID, ScopeKind: "default", Rates: m.Rates,
				Note: m.Note, EffectiveFrom: released,
				// 种子数据里有 warn 级问题时也照样写入：它是开发数据，
				// 而 warn 的意义是"让人看一眼"，命令行里没人可看。
				// **error 级问题依然会被拒绝**，这正是我们要的。
				Confirm: true,
			}); err != nil {
				return fmt.Errorf("seed price for %s/%s: %w", m.Provider, m.Slug, err)
			}
		}

		// 发布放在价目之后：没有价目的模型发布会被拒绝，
		// 那正是 M1a 的判据（计价 §3.6-③）。
		if m.Status == "listed" || m.Status == "canary" {
			if _, err := svc.Publish(ctx, actor, created.ID, m.Status, nil); err != nil {
				return fmt.Errorf("publish %s/%s: %w", m.Provider, m.Slug, err)
			}
		}
	}

	for _, b := range d.Benchmarks {
		lastRun, err := parseSeedTime(b.LastRunAt)
		if err != nil {
			return err
		}
		benchmarkID, err := seeder.UpsertBenchmark(ctx, postgres.SeedBenchmark{
			Slug: b.Slug, Name: b.Name, Category: b.Category, Description: b.Description,
			Methodology: b.Methodology, Configuration: b.Configuration,
			RunCount: b.RunCount, LastRunAt: lastRun,
		})
		if err != nil {
			return err
		}
		for _, res := range b.Results {
			modelID, ok := modelIDs[res.Model]
			if !ok {
				return fmt.Errorf("benchmark %s references unknown model %s", b.Slug, res.Model)
			}
			if err := seeder.UpsertBenchmarkResult(ctx, postgres.SeedBenchmarkResult{
				BenchmarkID: benchmarkID, ModelID: modelID, Score: res.Score,
				Percentile: res.Percentile, CostNano: res.CostNano,
				DurationMs: res.DurationMs, SampleCount: res.SampleCount,
			}); err != nil {
				return err
			}
		}
	}

	for _, f := range d.FAQ {
		modelID, ok := modelIDs[f.Model]
		if !ok {
			return fmt.Errorf("faq references unknown model %s", f.Model)
		}
		for i, item := range f.Items {
			if err := seeder.UpsertFAQ(ctx, modelID, item.Question, item.Answer, int32(i)); err != nil {
				return err
			}
		}
	}

	for _, doc := range d.Docs {
		if err := seeder.UpsertDocsPage(ctx, postgres.SeedDocsPage{
			Slug: doc.Slug, GroupTitle: doc.Group, GroupPosition: doc.GroupPosition,
			Position: doc.Position, Title: doc.Title, Description: doc.Description,
			Badge: doc.Badge, Body: doc.Body,
		}); err != nil {
			return err
		}
	}

	fmt.Printf("seeded %d providers, %d models, %d benchmarks, %d docs pages\n",
		len(d.Providers), len(d.Models), len(d.Benchmarks), len(d.Docs))
	return nil
}

func parseSeedTime(s string) (time.Time, error) {
	if strings.TrimSpace(s) == "" {
		return time.Now().UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid timestamp %q in seed file: %w", s, err)
	}
	return t, nil
}
