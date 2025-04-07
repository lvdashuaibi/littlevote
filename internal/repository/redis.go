package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"github.com/lvdashuaibi/littlevote/config"
	"github.com/lvdashuaibi/littlevote/internal/model"
)

const (
	// Redis键前缀
	UserVoteKey       = "user:vote:"
	TicketKey         = "ticket:"
	TicketVersionKey  = "ticket:newest:version"
	TicketLockKey     = "ticket:lock:"
	TicketProducerKey = "ticket:producer:lock"

	// Lua脚本
	DecrementTicketUsageScript = `
		-- 获取剩余使用次数
		local remaining = tonumber(redis.call('HGET', KEYS[1], 'remainingUsages'))
		if not remaining then
			return {-1, "票据数据损坏"}
		end
		
		-- 检查剩余使用次数
		if remaining <= 0 then
			return {-1, "票据使用次数已耗尽"}
		end
		
		-- 减少使用次数并更新
		remaining = remaining - 1
		redis.call('HSET', KEYS[1], 'remainingUsages', remaining)
		
		-- 返回更新后的剩余次数
		return {0, remaining}
	`
)

type RedisRepository struct {
	client       *redis.Client
	ctx          context.Context
	scriptHashes map[string]string // 存储脚本SHA1哈希值
}

func NewRedisRepository() (*RedisRepository, error) {
	ctx := context.Background()

	// 创建Redis客户端（普通客户端，用于数据存储）
	client := redis.NewClient(&redis.Options{
		Addr:         config.AppConfig.Redis.DataAddress,
		Password:     config.AppConfig.Redis.Password,
		DB:           config.AppConfig.Redis.DB,
		PoolSize:     config.AppConfig.Redis.PoolSize,
		MaxRetries:   config.AppConfig.Redis.MaxRetries,
		DialTimeout:  config.AppConfig.Redis.Timeout,
		ReadTimeout:  config.AppConfig.Redis.Timeout,
		WriteTimeout: config.AppConfig.Redis.Timeout,
	})

	// 测试连接
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("Redis数据节点连接测试失败: %w", err)
	}

	repo := &RedisRepository{
		client:       client,
		ctx:          ctx,
		scriptHashes: make(map[string]string),
	}

	// 预加载Lua脚本
	if err := repo.preloadScripts(); err != nil {
		return nil, fmt.Errorf("预加载Lua脚本失败: %w", err)
	}

	return repo, nil
}

// preloadScripts 预加载所有Lua脚本
func (r *RedisRepository) preloadScripts() error {
	// 预加载减少票据使用次数的脚本
	sha1, err := r.client.ScriptLoad(r.ctx, DecrementTicketUsageScript).Result()
	if err != nil {
		return fmt.Errorf("加载票据使用次数脚本失败: %w", err)
	}
	r.scriptHashes["decrementTicketUsage"] = sha1

	return nil
}

// GetUserVote 从缓存获取用户票数
func (r *RedisRepository) GetUserVote(username string) (*model.UserVote, bool, error) {
	key := UserVoteKey + username
	data, err := r.client.Get(r.ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			return nil, false, nil // 缓存未命中
		}
		return nil, false, fmt.Errorf("获取用户票数缓存失败: %w", err)
	}

	var userVote model.UserVote
	if err := json.Unmarshal([]byte(data), &userVote); err != nil {
		return nil, false, fmt.Errorf("解析用户票数缓存失败: %w", err)
	}

	return &userVote, true, nil
}

// SetUserVote 设置用户票数缓存
func (r *RedisRepository) SetUserVote(userVote *model.UserVote) error {
	key := UserVoteKey + userVote.Username
	data, err := json.Marshal(userVote)
	if err != nil {
		return fmt.Errorf("序列化用户票数失败: %w", err)
	}

	// 设置缓存，有效期1小时
	if err := r.client.Set(r.ctx, key, data, time.Hour).Err(); err != nil {
		return fmt.Errorf("设置用户票数缓存失败: %w", err)
	}

	return nil
}

// DeleteUserVoteCache 删除用户票数缓存
func (r *RedisRepository) DeleteUserVoteCache(username string) error {
	key := UserVoteKey + username
	if err := r.client.Del(r.ctx, key).Err(); err != nil {
		return fmt.Errorf("删除用户票数缓存失败: %w", err)
	}
	return nil
}

// GetNewestTicketVersion 获取最新票据版本
func (r *RedisRepository) GetNewestTicketVersion() (string, error) {
	version, err := r.client.Get(r.ctx, TicketVersionKey).Result()
	if err != nil {
		if err == redis.Nil {
			return "", nil // 版本不存在
		}
		return "", fmt.Errorf("获取最新票据版本失败: %w", err)
	}
	return version, nil
}

// SetNewestTicketVersion 设置最新票据版本
func (r *RedisRepository) SetNewestTicketVersion(version string) error {
	if err := r.client.Set(r.ctx, TicketVersionKey, version, 0).Err(); err != nil {
		return fmt.Errorf("设置最新票据版本失败: %w", err)
	}
	return nil
}

// GetTicket 获取票据
func (r *RedisRepository) GetTicket(version string) (*model.Ticket, error) {
	key := TicketKey + version
	//fmt.Println("GetTicket key:", key)
	data, err := r.client.HGetAll(r.ctx, key).Result()
	if err != nil {
		return nil, fmt.Errorf("获取票据失败: %w", err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("票据不存在")
	}

	// 解析票据数据
	ticket := &model.Ticket{
		Version: version,
		Value:   data["value"],
	}

	// 解析剩余使用次数
	if data["remainingUsages"] != "" {
		var remainingUsages int
		if _, err := fmt.Sscanf(data["remainingUsages"], "%d", &remainingUsages); err != nil {
			return nil, fmt.Errorf("解析票据剩余使用次数失败: %w", err)
		}
		ticket.RemainingUsages = remainingUsages
	}

	// 解析过期时间
	if data["expiresAt"] != "" {
		expiresAt, err := time.Parse(time.RFC3339, data["expiresAt"])
		if err != nil {
			return nil, fmt.Errorf("解析票据过期时间失败: %w", err)
		}
		ticket.ExpiresAt = expiresAt
	}

	// 解析创建时间
	if data["createdAt"] != "" {
		createdAt, err := time.Parse(time.RFC3339, data["createdAt"])
		if err != nil {
			return nil, fmt.Errorf("解析票据创建时间失败: %w", err)
		}
		ticket.CreatedAt = createdAt
	}

	return ticket, nil
}

// CreateTicket 创建新票据
func (r *RedisRepository) CreateTicket(ticket *model.Ticket) error {
	key := TicketKey + ticket.Version
	fmt.Println("CreateTicket key:", key)
	// 准备票据数据
	data := map[string]interface{}{
		"value":           ticket.Value,
		"remainingUsages": ticket.RemainingUsages,
		"expiresAt":       ticket.ExpiresAt.Format(time.RFC3339),
		"createdAt":       ticket.CreatedAt.Format(time.RFC3339),
	}

	// Redis 过期时间设置为10s
	expires := time.Second * 10

	// 设置票据，并设置过期时间
	pipe := r.client.Pipeline()
	pipe.HMSet(r.ctx, key, data)
	pipe.Expire(r.ctx, key, expires)
	_, err := pipe.Exec(r.ctx)
	if err != nil {
		return fmt.Errorf("创建票据失败: %w", err)
	}

	return nil
}

// UpdateTicketRemainingUsages 更新票据剩余使用次数
func (r *RedisRepository) UpdateTicketRemainingUsages(version string, remainingUsages int) error {
	key := TicketKey + version
	if err := r.client.HSet(r.ctx, key, "remainingUsages", remainingUsages).Err(); err != nil {
		return fmt.Errorf("更新票据剩余使用次数失败: %w", err)
	}
	return nil
}

// Close 关闭Redis连接
func (r *RedisRepository) Close() error {
	return r.client.Close()
}

// ValidateTicket 校验票据有效性
func (r *RedisRepository) ValidateTicket(ticket *model.Ticket) (bool, error) {
	// 获取最新版本
	newestVersion, err := r.GetNewestTicketVersion()
	if err != nil {
		return false, fmt.Errorf("获取最新票据版本失败: %w", err)
	}

	// 检查版本是否一致
	if ticket.Version != newestVersion {
		return false, fmt.Errorf("票据版本已过期，当前: %s, 最新: %s", ticket.Version, newestVersion)
	}

	// 获取票据
	storedTicket, err := r.GetTicket(ticket.Version)
	if err != nil {
		return false, fmt.Errorf("获取票据失败: %w", err)
	}

	// 检查票据值是否一致
	if ticket.Value != storedTicket.Value {
		return false, fmt.Errorf("票据值不匹配")
	}

	return true, nil
}

// DecrementTicketUsage 使用预加载的Lua脚本减少票据的使用次数，保证原子性
func (r *RedisRepository) DecrementTicketUsage(version string) (int, error) {
	key := TicketKey + version

	// 获取预加载脚本的SHA1哈希值
	sha1, ok := r.scriptHashes["decrementTicketUsage"]
	if !ok {
		return 0, fmt.Errorf("脚本未预加载")
	}

	// 使用EVALSHA执行脚本
	var result interface{}
	var err error

	// 尝试使用EVALSHA执行
	result, err = r.client.EvalSha(r.ctx, sha1, []string{key, TicketVersionKey}, version).Result()
	if err != nil {
		// 如果脚本不存在，重新加载并再次尝试
		if err.Error() == "NOSCRIPT No matching script. Please use EVAL." {
			// 重新加载脚本
			sha1, err = r.client.ScriptLoad(r.ctx, DecrementTicketUsageScript).Result()
			if err != nil {
				return 0, fmt.Errorf("重新加载票据使用次数脚本失败: %w", err)
			}
			r.scriptHashes["decrementTicketUsage"] = sha1

			// 再次尝试执行
			result, err = r.client.EvalSha(r.ctx, sha1, []string{key, TicketVersionKey}, version).Result()
			if err != nil {
				return 0, fmt.Errorf("执行票据使用次数脚本失败: %w", err)
			}
		} else {
			return 0, fmt.Errorf("执行票据使用次数脚本失败: %w", err)
		}
	}

	// 解析结果
	resultSlice, ok := result.([]interface{})
	if !ok {
		return 0, fmt.Errorf("LUA脚本返回类型错误")
	}

	// 检查结果长度
	if len(resultSlice) < 2 {
		return 0, fmt.Errorf("LUA脚本返回格式错误")
	}

	// 检查状态码
	status, ok := resultSlice[0].(int64)
	if !ok {
		return 0, fmt.Errorf("LUA脚本返回状态码类型错误")
	}

	// 如果状态码不为0，表示出错
	if status != 0 {
		errorMsg, _ := resultSlice[1].(string)
		return 0, fmt.Errorf("%s", errorMsg)
	}

	// 获取剩余次数
	remaining, ok := resultSlice[1].(int64)
	if !ok {
		return 0, fmt.Errorf("LUA脚本返回剩余次数类型错误")
	}

	return int(remaining), nil
}
