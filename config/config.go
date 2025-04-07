package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	MySQL   MySQLConfig   `mapstructure:"mysql"`
	Redis   RedisConfig   `mapstructure:"redis"`
	Kafka   KafkaConfig   `mapstructure:"kafka"`
	Ticket  TicketConfig  `mapstructure:"ticket"`
	ETCD    ETCDConfig    `mapstructure:"etcd"`
	GraphQL GraphQLConfig `mapstructure:"graphql"`
}

type ServerConfig struct {
	Port int `mapstructure:"port"`
}

type MySQLConfig struct {
	Master       string `mapstructure:"master"`
	Slave        string `mapstructure:"slave"`
	MaxOpenConns int    `mapstructure:"max_open_conns"`
	MaxIdleConns int    `mapstructure:"max_idle_conns"`
}

type RedisConfig struct {
	// 数据存储Redis
	DataAddress string        `mapstructure:"data_address"`
	Password    string        `mapstructure:"password"`
	DB          int           `mapstructure:"db"`
	PoolSize    int           `mapstructure:"pool_size"`
	MaxRetries  int           `mapstructure:"max_retries"`
	Timeout     time.Duration `mapstructure:"timeout"`

	// Redlock使用的Redis节点
	LockAddresses []string `mapstructure:"lock_addresses"`
}

type KafkaConfig struct {
	Brokers   []string `mapstructure:"brokers"`
	Topic     string   `mapstructure:"topic"`
	Partition int      `mapstructure:"partition"`
	GroupID   string   `mapstructure:"group_id"`
}

type TicketConfig struct {
	RefreshInterval time.Duration `mapstructure:"refresh_interval"`
	MaxUsageCount   int           `mapstructure:"max_usage_count"`
	LockTimeout     time.Duration `mapstructure:"lock_timeout"`
	LockRetryCount  int           `mapstructure:"lock_retry_count"`
}

type ETCDConfig struct {
	Endpoints      []string      `mapstructure:"endpoints"`
	DialTimeout    time.Duration `mapstructure:"dial_timeout"`
	RequestTimeout time.Duration `mapstructure:"request_timeout"`
	SessionTTL     time.Duration `mapstructure:"session_ttl"`
}

type GraphQLConfig struct {
	Path string `mapstructure:"path"`
}

var AppConfig Config

// LoadConfig 加载配置文件
func LoadConfig(configPath string) (*Config, error) {
	viper.SetConfigFile(configPath)
	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("读取配置文件失败: %w", err)
	}

	if err := viper.Unmarshal(&AppConfig); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &AppConfig, nil
}
