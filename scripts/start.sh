#!/bin/bash

# 确保脚本出错时停止执行
set -e

echo "=== 启动 Little Vote 系统 ==="

# 启动所有依赖服务
echo "启动 MySQL, Redis, Kafka, ETCD..."
docker-compose up -d


# 初始化Kafka主题
echo "创建Kafka主题..."
docker exec -it kafka kafka-topics --create --if-not-exists --topic vote-events --bootstrap-server kafka:29092 --partitions 8 --replication-factor 1

echo "系统启动完成!"
echo "MySQL主节点: localhost:3306"
echo "MySQL从节点: localhost:3307"
echo "Redis数据节点: localhost:6382"
echo "Redis锁节点1: localhost:6379"
echo "Redis锁节点2: localhost:6380"
echo "Redis锁节点3: localhost:6381"
echo "Kafka: localhost:9092"
echo "ETCD: localhost:2379"

# 启动应用
cd ..
echo "启动 Little Vote 应用..."
go run cmd/main.go 