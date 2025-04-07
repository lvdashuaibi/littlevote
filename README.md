# Little Vote 系统设计文档

## 1. 系统概述

Little Vote是一个高并发分布式投票系统，通过票据机制控制投票行为，每2秒生成一个新的票据，票据在有效期内可用于投票操作。系统采用了多种高可用和分布式技术，包括分布式锁、消息队列、缓存等，在4核4G的机器上能够达到3200QPS的高性能。

## 2. 架构设计

### 2.1 整体架构

系统采用三层架构设计：
- **存储层**：MySQL、Redis、Kafka
- **服务层**：票据服务、投票服务、分布式锁服务等
- **接口层**：GraphQL API

### 2.2 关键组件

1. **MySQL**：
   - 主从复制架构
   - 存储用户票数、投票日志、票据历史等数据
   - 表设计：`user_votes`, `vote_logs`, `ticket_history`, `tickets`

2. **Redis**：
   - 票据的主要存储和高速缓存层
   - 存储票据信息和版本
   - 使用Lua脚本实现原子操作

3. **Kafka**：
   - 异步处理投票事件和票据使用更新
   - 保证消息的可靠性和顺序性
   - 使用8个分区实现高并发消费

4. **票据服务**：
   - 定时生成新票据（每2秒）
   - 通过版本控制票据有效性
   - 使用分布式锁确保多实例环境下只有一个实例生成票据

5. **投票服务**：
   - 验证票据有效性
   - 处理投票请求
   - 更新用户票数
   - 清除相关缓存

6. **GraphQL API**：
   - 提供GraphQL风格接口
   - 处理票据获取、投票操作、查询票数等功能

## 3. 关键技术详解

### 3.1 票据机制

1. **生成机制**：
   - 票据由服务端每2秒生成一个
   - 每个票据包含：值、版本、剩余使用次数(默认为1000)、过期时间、创建时间
   - 票据同时存储在Redis和MySQL中，Redis作为高速缓存层

2. **版本控制**：
   - 使用`newestTicketVersion`字段存储在Redis中
   - 每次生成新票据，更新此版本号
   - 投票时验证票据版本是否为最新

3. **使用控制**：
   - 票据有使用次数限制（默认1000次）
   - 使用Lua脚本在Redis中原子操作减少使用次数，如果能够操作成功再去操作MySQL
   - 通过Kafka异步更新MySQL中的使用记录

### 3.2 分布式锁

1. **票据生成锁**：
   - 使用ETCD实现分布式锁，在并发性低的场景下保证高可用
   - 锁名为`ticket:producer:lock`
   - 确保多实例环境下只有一个实例生成票据

2. **无锁票据使用**：
   - 获取和使用票据时不需要加分布式锁
   - 通过Redis的Lua脚本实现原子操作
   - 大幅提高系统并发性能

### 3.3 数据一致性保障

1. **原子操作**：
   - 使用Redis Lua脚本确保票据使用的原子性
   - 使用数据库事务确保MySQL数据一致性

2. **缓存策略**：
   - Redis作为主要操作层，提供高速读写
   - ticket缓存：Redis操作成功后通过Kafka异步更新MySQL
   - 用户信息缓存：使用缓存-穿透模式，先查缓存再查数据库，投票后主动删除相关用户的缓存，缓存自动设置过期时间(1小时)

3. **异步消息处理**：
   - 投票操作和票据使用记录通过Kafka异步处理
   - 8个分区并行处理，提高系统吞吐量
   - 如果消息发送失败，则同步操作数据库保证系统可用

## 4. 容错与扩展性

### 4.1 容错设计

1. **MySQL主从架构**：
   - 从库连接失败时自动使用主库
   - 支持读写分离，提高系统可用性

2. **Redis优先策略**：
   - 票据操作优先通过Redis完成
   - Redis失败时可降级使用MySQL操作
   - 确保系统在部分组件故障时仍能提供服务

3. **错误恢复机制**：
   - Redis与MySQL数据自动同步
   - 票据生成锁支持自动续期和故障恢复

### 4.2 扩展性设计

1. **水平扩展**：
   - 服务无状态设计，可部署多个实例
   - Kafka支持分区扩展，提高并行处理能力

2. **容量扩展**：
   - 票据使用次数可配置（当前1000次）
   - 系统关键参数均可通过配置文件调整

## 5. 性能优化

1. **无锁设计**：
   - 票据获取和使用过程无需分布式锁
   - 使用Lua脚本在Redis中实现原子操作
   - 显著提高并发性能

2. **异步处理**：
   - 通过Kafka异步处理投票事件和票据使用记录
   - 减少响应时间，提高系统吞吐量

3. **分区并行处理**：
   - Kafka使用8个分区支持并行消费
   - 与4核CPU匹配，充分利用处理器资源

4. **高性能缓存**：
   - Redis连接池大小为100，匹配高并发需求
   - 票据信息和用户数据缓存在Redis，减少数据库查询

## 6. 安全性

1. **票据安全**：
   - 票据生成使用密码学安全的随机数
   - 票据有版本控制和有效期验证

2. **输入验证**：
   - 用户名限制在A-Z范围内
   - 请求参数格式严格验证

## 7. 部署架构

系统使用Docker Compose进行部署，包括：
- MySQL主从实例（读写分离）
- Redis实例（高性能缓存）
- Kafka & Zookeeper（8个分区）
- 应用服务（多实例）

## 8. 性能数据

单实例系统在4核4G的机器上能够实现以下性能：
- 并发获取ticket并投票处理能力：3200+ QPS
- 并发获取用户票数能力 : 5000+ QPS
- 票据使用次数：1000次/票据
- Kafka消费能力：8个分区并行处理

一键启动和停止脚本简化了部署过程。 