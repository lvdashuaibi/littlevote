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
   - Redis连接池大小为5000，匹配高并发需求
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
- 获取用户票数接口性能 : 5000+ QPS
- 获取ticket接口性能：3600+ QPS
- 票据使用次数：1000次/票据
- Kafka消费能力：8个分区并行处理

## 9. 启动方式
- 一键启动和停止脚本简化了部署过程。 
- 可采用scripts/start.sh 和 scripts/stop.sh一键启动停止

## 10. 性能优化日志
- 以下性能结果是针对于4核4G的机器，单实例部署Redis，并且访问每次投票都获取一次ticket得到的结果。
- 修改ticket从Redlock改为ETCD Lock之后，性能由 200+ QPS 提升到 700+ QPS
- 修改ticekt操作由加分布式锁改为在Redis中使用LUA原子操作更新，并异步更新MySQL之后，性能由 700+ QPS提升到 3200+ QPS
- Redis连接池大小由100改为3000之后，性能由3200+ QPS 提升到 3800+ QPS

## 11. 功能完整性验证设计
- **ticket使用次数验证**：将单张ticket使用次数设置为100时，采用wrk工具使用16线程1000并发请求进行投票，能够确保30内投出的票为750整，说明ticket使用次数限制是成功的。
- **高并发场景下投票数量验证**：对单个用户、多个用户采用上述高并发配置进行投票的时候，将单张ticket使用次数限制提高到100000，也就是在现有系统的基础上无法达到的数值，得到数据库中用户票数与服务端响应成功数是一致的，说明系统投票功能没有出现并发问题。
- **ticekt过期时间验证**：还是同样的高并发场景下，获取ticket等待2s之后再进行投票，测试30s后系统成功投出0张票，说明ticket的合法性验证是正确的。

## 12. API接口文档

Little Vote系统使用GraphQL API提供服务。GraphQL端点默认为`/graphql`，并提供了一个交互式Playground界面用于测试。

### 12.1 数据类型

#### UserVote
用户投票信息类型
```graphql
type UserVote {
  username: String!      # 用户名（A-Z）
  votes: Int!            # 用户的票数
  updatedAt: String!     # 最后更新时间（RFC3339格式）
}
```

#### Ticket
票据信息类型
```graphql
type Ticket {
  value: String!         # 票据值
  version: String!       # 票据版本
  remainingUsages: Int!  # 剩余使用次数
  expiresAt: String!     # 过期时间（RFC3339格式）
  createdAt: String!     # 创建时间（RFC3339格式）
}
```

#### VoteResponse
投票操作响应类型
```graphql
type VoteResponse {
  success: Boolean!      # 操作是否成功
  message: String!       # 操作结果消息
  usernames: [String!]!  # 投票的用户名列表
  timestamp: String!     # 操作时间戳（RFC3339格式）
}
```

### 12.2 查询接口

#### 获取当前票据
获取当前可用的最新票据。
```graphql
query {
  getTicket {
    value
    version
    remainingUsages
    expiresAt
    createdAt
  }
}
```

#### 查询用户票数
查询指定用户的当前票数。
```graphql
query {
  getUserVotes(username: "A") {
    username
    votes
    updatedAt
  }
}
```

#### 查询所有用户票数
查询所有用户的当前票数。
```graphql
query {
  getAllUserVotes {
    username
    votes
    updatedAt
  }
}
```

### 12.3 变更接口

#### 投票
使用指定票据为一个或多个用户投票。
```graphql
mutation {
  vote(input: {
    usernames: ["A", "B"],
    ticket: {
      value: "例:xxxxxxxxxx",
      version: "例:1714183361",
      remainingUsages: 999,
      expiresAt: "2023-04-27T15:29:21+08:00",
      createdAt: "2023-04-27T15:27:21+08:00"
    }
  }) {
    success
    message
    usernames
    timestamp
  }
}
```

#### 获取票据并立即投票
一步完成获取票据并为一个或多个用户投票的操作。
```graphql
mutation {
  ticketAndVote(usernames: ["A", "B"]) {
    success
    message
    usernames
    timestamp
  }
}
```

### 12.4 错误处理

API中的错误分为两类：
1. **GraphQL错误**：这些错误会在响应的`errors`字段中返回
2. **业务逻辑错误**：这些错误会通过响应对象的`success`和`message`字段返回

常见错误包括：
- 票据已过期或版本不匹配
- 票据使用次数已耗尽
- 用户名格式不正确（必须为A-Z）
- 系统内部错误

### 12.5 使用示例

**获取票据并投票的完整流程示例**：

```graphql
# 第一步：获取票据
query {
  getTicket {
    value
    version
    remainingUsages
    expiresAt
    createdAt
  }
}

# 第二步：使用获取的票据进行投票
mutation {
  vote(input: {
    usernames: ["A"],
    ticket: {
      value: "获取的票据value",
      version: "获取的票据version",
      remainingUsages: 获取的票据remainingUsages,
      expiresAt: "获取的票据expiresAt",
      createdAt: "获取的票据createdAt"
    }
  }) {
    success
    message
    usernames
    timestamp
  }
}

# 或者使用一步完成的方式
mutation {
  ticketAndVote(usernames: ["A"]) {
    success
    message
    usernames
    timestamp
  }
}
```

