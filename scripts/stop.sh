#!/bin/bash

echo "=== 停止 Little Vote 系统 ==="

# 停止应用(如果有PID文件)
if [ -f "../.pid" ]; then
  PID=$(cat "../.pid")
  if ps -p $PID > /dev/null; then
    echo "关闭 Little Vote 应用 (PID: $PID)..."
    kill $PID
  fi
  rm -f "../.pid"
fi

# 停止Docker服务
echo "关闭 MySQL, Redis, Kafka..."
docker-compose down

echo "系统已完全停止!" 