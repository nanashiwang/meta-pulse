#!/usr/bin/env python3
"""Retired command: never connect to the database or Docker."""
import sys
if __name__ == "__main__":
    print("API 消费摄入已退役；请移除 --accept-ingest，改为检查 Worker 健康与奖励结算。", file=sys.stderr)
    sys.exit(1)
