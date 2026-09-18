// Package kafka 提供了与 Kafka 消息队列交互的功能。
package kafka

import (
	"context"
	"encoding/json"
	"github.com/ericthz/zebra-rag/internal/config"
	"github.com/ericthz/zebra-rag/pkg/log"
	"github.com/ericthz/zebra-rag/pkg/tasks"
	"sync"
	"time"

	"github.com/segmentio/kafka-go"
)

// TaskProcessor defines the interface for any service that can process a task.
// This decouples the Kafka consumer from the concrete pipeline implementation.
type TaskProcessor interface {
	Process(ctx context.Context, task tasks.FileProcessingTask) error
}

var producer *kafka.Writer

// InitProducer 初始化 Kafka 生产者。
func InitProducer(cfg config.KafkaConfig) {
	producer = &kafka.Writer{
		Addr:     kafka.TCP(cfg.Brokers),
		Topic:    cfg.Topic,
		Balancer: &kafka.LeastBytes{},
	}
	log.Info("Kafka 生产者初始化成功")
}

// ProduceFileTask 发送一个文件处理任务到 Kafka。
func ProduceFileTask(task tasks.FileProcessingTask) error {
	taskBytes, err := json.Marshal(task)
	if err != nil {
		return err
	}

	err = producer.WriteMessages(context.Background(),
		kafka.Message{
			Value: taskBytes,
		},
	)
	return err
}

// StartConsumer 启动 Kafka 消费者组来处理文件任务。
// 使用多个同 GroupID 的消费者实例，按分区并行处理；单分区内保持顺序。
func StartConsumer(cfg config.KafkaConfig, processor TaskProcessor) {
	const consumerNum = 3
	var wg sync.WaitGroup
	for i := 0; i < consumerNum; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			consume(cfg, processor, id)
		}(i)
	}
	wg.Wait()
	log.Info("Kafka 消费者组已全部退出")
}

// consume 运行单个消费者实例：串行消费 + 同一条消息失败重试（指数退避，最多 maxAttempts 次）。
// 修复：原先失败后并不重投本消息，退避睡眠无实际效果；现改为在同一会话内对同一条消息真实重试，
// 全部尝试仍失败才提交 offset 跳过（消息丢弃，等价简化版 DLQ），避免无限阻塞队列。
func consume(cfg config.KafkaConfig, processor TaskProcessor, id int) {
	const maxAttempts = 3
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:  []string{cfg.Brokers},
		Topic:    cfg.Topic,
		GroupID:  "zebrarag-consumer",
		MinBytes: 10e3, // 10KB
		MaxBytes: 10e6, // 10MB
	})
	defer r.Close()

	log.Infof("Kafka 消费者 #%d 已启动，正在监听主题 '%s'", id, cfg.Topic)

	for {
		m, err := r.FetchMessage(context.Background())
		if err != nil {
			log.Errorf("消费者 #%d 从 Kafka 读取消息失败: %v", id, err)
			break // 退出循环，可能需要重启策略
		}

		log.Infof("消费者 #%d 收到 Kafka 消息: partition=%d offset=%d", id, m.Partition, m.Offset)

		var task tasks.FileProcessingTask
		if err := json.Unmarshal(m.Value, &task); err != nil {
			log.Errorf("消费者 #%d 无法解析 Kafka 消息: %v, value: %s", id, err, string(m.Value))
			// 消息格式错误，直接提交，避免阻塞队列
			if err := r.CommitMessages(context.Background(), m); err != nil {
				log.Errorf("消费者 #%d 提交错误消息失败: %v", id, err)
			}
			continue
		}

		log.Infof("消费者 #%d 开始处理文件任务: MD5=%s, FileName=%s", id, task.FileMD5, task.FileName)
		var lastErr error
		for attempt := 1; attempt <= maxAttempts; attempt++ {
			if attempt > 1 {
				// 同一条消息在会话内真实重试：先退避再重跑 Process
				backoff := time.Duration(1<<(attempt-1)) * time.Second
				if backoff > 30*time.Second {
					backoff = 30 * time.Second
				}
				log.Warnf("消费者 #%d 处理失败(第 %d 次)。等待 %dms 后重试本条消息: MD5=%s", id, attempt-1, backoff.Milliseconds(), task.FileMD5)
				time.Sleep(backoff)
			}
			if err := processor.Process(context.Background(), task); err != nil {
				lastErr = err
				log.Errorf("消费者 #%d 处理文件任务失败(尝试 %d/%d): MD5=%s, Error: %v", id, attempt, maxAttempts, task.FileMD5, err)
				continue
			}
			lastErr = nil
			break
		}

		if lastErr != nil {
			// 多次失败，提交 offset 终止重试（当前以「重试耗尽 + 跳过」实现简化 DLQ 语义）
			log.Errorf("消费者 #%d 文件任务已尝试 %d 次仍失败，提交 offset 跳过: MD5=%s, Error: %v", id, maxAttempts, task.FileMD5, lastErr)
			if err := r.CommitMessages(context.Background(), m); err != nil {
				log.Errorf("消费者 #%d 提交 Kafka 消息 offset 失败: %v", id, err)
			}
			continue
		}

		log.Infof("消费者 #%d 文件任务处理成功: MD5=%s", id, task.FileMD5)
		// 任务处理成功后，手动提交 offset
		if err := r.CommitMessages(context.Background(), m); err != nil {
			log.Errorf("消费者 #%d 提交 Kafka 消息 offset 失败: %v", id, err)
		}
	}
}
