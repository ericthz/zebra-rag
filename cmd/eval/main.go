// Command eval 是 RAG 离线评测运行器。
// 运行前提：MySQL / Redis / ES / Embedding 服务已启动（与主服务共用配置）。
// 用法：
//
//	go run ./cmd/eval -config ./configs/config.yaml -dataset ./eval/dataset.json -k 10
//	go run ./cmd/eval -answers ./answers.txt   # 提供生成答案，额外输出生成层指标
//
// 评测数据集格式见 eval/dataset.json。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/eval"
	"github.com/ericthz/zebra-rag/internal/infra/database"
	"github.com/ericthz/zebra-rag/internal/infra/embedding"
	"github.com/ericthz/zebra-rag/internal/infra/es"
	"github.com/ericthz/zebra-rag/internal/infra/rerank"
	"github.com/ericthz/zebra-rag/internal/model"
	"github.com/ericthz/zebra-rag/internal/repository"
	"github.com/ericthz/zebra-rag/internal/service"
	"github.com/ericthz/zebra-rag/pkg/log"
)

func main() {
	configPath := flag.String("config", config.ResolvePath("./configs/config.yaml"), "配置文件路径")
	datasetPath := flag.String("dataset", "./eval/dataset.json", "评测数据集路径")
	answersPath := flag.String("answers", "", "生成答案文件（每行一条，可选，用于生成层指标）")
	topK := flag.Int("k", 10, "检索 top-k")
	flag.Parse()

	config.Init(*configPath)
	cfg := config.Conf
	log.Init(cfg.Log.Level, cfg.Log.Format, cfg.Log.OutputPath)

	database.InitMySQL(cfg.Database.MySQL.DSN)
	database.InitRedis(cfg.Database.Redis.Addr, cfg.Database.Redis.Password, cfg.Database.Redis.DB)
	if err := es.InitES(cfg.Elasticsearch); err != nil {
		log.Fatalf("ES 初始化失败: %v", err)
	}

	userRepo := repository.NewUserRepository(database.DB)
	orgTagRepo := repository.NewOrgTagRepository(database.DB)
	uploadRepo := repository.NewUploadRepository(database.DB, database.RDB)
	conversationRepo := repository.NewConversationRepository(database.RDB)
	userService := service.NewUserService(userRepo, orgTagRepo, conversationRepo, uploadRepo, nil)
	embeddingClient := embedding.NewClient(cfg.Embedding)

	var rerankClient *rerank.Client
	if cfg.Rerank.Enabled {
		rerankClient = rerank.NewClient(cfg.Rerank.BaseURL, cfg.Rerank.Model, cfg.Rerank.Timeout)
	}
	searchService := service.NewSearchService(embeddingClient, es.ESClient, userService, uploadRepo, rerankClient, nil, nil)

	ds, err := eval.LoadDataset(*datasetPath)
	if err != nil {
		log.Fatalf("加载评测数据集失败: %v", err)
	}

	// 使用 admin 用户作为权限上下文（评测数据集多为公开文档）
	evalUser, err := userRepo.FindByUsername("admin")
	if err != nil {
		evalUser = &model.User{ID: 1, Username: "admin", Role: "ADMIN", OrgTags: "PRIVATE_admin", PrimaryOrg: "PRIVATE_admin"}
	}

	ctx := context.Background()
	var retrieved, expected [][]string
	skipped := 0
	for _, item := range ds.Items {
		// 无 expected_docs 的样本无法计算检索层指标，透明跳过并上报，避免拉低均值
		if len(item.ExpectedDocs) == 0 {
			skipped++
			log.Warnf("样本缺少 expected_docs，跳过检索层指标: %s", item.Question)
			continue
		}
		hits, err := searchService.HybridSearch(ctx, item.Question, *topK, evalUser)
		if err != nil {
			log.Warnf("查询失败，跳过: %s, error: %v", item.Question, err)
			continue
		}
		ids := make([]string, 0, len(hits))
		for _, h := range hits {
			ids = append(ids, h.FileMD5)
		}
		retrieved = append(retrieved, ids)
		expected = append(expected, item.ExpectedDocs)
	}

	rm := eval.ComputeRetrieval(retrieved, expected, *topK)
	cm := eval.ComputeContext(retrieved, expected, *topK)

	var gm eval.GenerationMetrics
	hasAnswers := *answersPath != ""
	if hasAnswers {
		answers := readLines(*answersPath)
		refs := make([]string, len(ds.Items))
		for i, it := range ds.Items {
			refs[i] = it.ReferenceAnswer
		}
		gm = eval.ComputeGeneration(answers, refs)
	}

	fmt.Println("==================== RAG 评测报告 ====================")
	fmt.Printf("数据集: %s（%d 条）\n", ds.Name, len(ds.Items))
	fmt.Printf("检索层  : recall@%d=%.4f  MRR=%.4f  hit-rate=%.4f\n", *topK, rm.RecallAtK, rm.MRR, rm.HitRate)
	fmt.Printf("上下文  : context_precision=%.4f  context_recall=%.4f\n", cm.ContextPrecision, cm.ContextRecall)
	fmt.Printf("有效样本: %d 条参与检索层指标（跳过 %d 条无 expected_docs 样本）\n", len(retrieved), skipped)
	if hasAnswers {
		fmt.Printf("生成层  : faithfulness=%.4f  answer_relevancy=%.4f\n", gm.Faithfulness, gm.AnswerRelevancy)
	} else {
		fmt.Println("生成层  : 未提供答案文件（-answers），跳过")
	}
	fmt.Println("======================================================")
}

func readLines(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		log.Fatalf("读取答案文件失败: %v", err)
	}
	var lines []string
	for _, l := range strings.Split(string(data), "\n") {
		if t := strings.TrimSpace(l); t != "" {
			lines = append(lines, t)
		}
	}
	return lines
}
