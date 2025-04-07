package kafka

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"github.com/lvdashuaibi/littlevote/config"
	"github.com/lvdashuaibi/littlevote/internal/model"
	"github.com/segmentio/kafka-go"
)

type Producer struct {
	writer         *kafka.Writer
	ctx            context.Context
	partitionCount int // 主题的分区数量
}

func NewProducer() (*Producer, error) {
	ctx := context.Background()

	// 获取分区数量
	conn, err := kafka.DialLeader(ctx, "tcp", config.AppConfig.Kafka.Brokers[0], config.AppConfig.Kafka.Topic, 0)
	if err != nil {
		return nil, fmt.Errorf("连接Kafka失败: %w", err)
	}
	defer conn.Close()

	partitions, err := conn.ReadPartitions()
	if err != nil {
		return nil, fmt.Errorf("读取分区信息失败: %w", err)
	}

	topicPartitions := 0
	for _, p := range partitions {
		if p.Topic == config.AppConfig.Kafka.Topic {
			topicPartitions++
		}
	}

	log.Printf("生产者检测到Kafka主题 %s 有 %d 个分区", config.AppConfig.Kafka.Topic, topicPartitions)

	// 使用Hash分区器，基于消息Key进行分区路由
	writer := &kafka.Writer{
		Addr:     kafka.TCP(config.AppConfig.Kafka.Brokers...),
		Topic:    config.AppConfig.Kafka.Topic,
		Balancer: &kafka.Hash{}, // 使用基于消息Key的Hash分区器
	}

	return &Producer{
		writer:         writer,
		ctx:            ctx,
		partitionCount: topicPartitions,
	}, nil
}

// SendVoteEvent 发送投票事件到Kafka
func (p *Producer) SendVoteEvent(event *model.VoteEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("序列化投票事件失败: %w", err)
	}

	// 使用username作为分区key，确保相同用户的投票事件进入同一分区
	// 如果有多个username，选择第一个作为路由key
	var key []byte
	if len(event.Usernames) > 0 {
		key = []byte(event.Usernames[0])
	} else {
		key = []byte(event.TicketVersion)
	}

	// 创建Kafka消息
	msg := kafka.Message{
		Key:   key,
		Value: data,
		Time:  time.Now(),
	}

	// 发送消息
	if err := p.writer.WriteMessages(p.ctx, msg); err != nil {
		return fmt.Errorf("发送投票事件失败: %w", err)
	}

	//log.Printf("已发送投票事件: 路由键=%s, 票据版本=%s, 用户数=%d",
	//	string(key), event.TicketVersion, len(event.Usernames))
	return nil
}

// Close 关闭Kafka生产者
func (p *Producer) Close() error {
	return p.writer.Close()
}
