// Command worker 是文档处理 Worker：独立进程运行 Kafka 消费者，专职执行入库流水线，
// 与 API 网关（cmd/server）解耦部署，便于按负载独立扩容。
package main

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/ericthz/zebra-rag/internal/bootstrap"
	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/internal/infra/kafka"
	"github.com/ericthz/zebra-rag/pkg/log"
)

func main() {
	// 1. 配置与日志
	config.Init(config.ResolvePath("./configs/config.yaml"))
	cfg := config.Conf

	log.Init(cfg.Log.Level, cfg.Log.Format, cfg.Log.OutputPath)
	defer log.Sync()
	log.Info("worker 日志记录器初始化成功")

	// 2. 装配（复用同一套依赖，与网关保持一致）
	app, err := bootstrap.New(&cfg)
	if err != nil {
		log.Fatalf("应用装配失败: %v", err)
	}

	// 3. 消费队列，在后台运行；主协程等待停机信号
	done := make(chan struct{})
	go func() {
		kafka.StartConsumer(cfg.Kafka, app.Processor)
		close(done)
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-quit:
		log.Info("worker 收到停机信号，正在退出...")
	case <-done:
		log.Info("Kafka 消费者已全部退出")
	}
}
