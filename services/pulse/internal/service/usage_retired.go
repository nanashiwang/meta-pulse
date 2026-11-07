package service

// DefaultUsageCursorName identifies historical audit data only. API usage
// ingestion is retired; no production service reads or advances this cursor.
const DefaultUsageCursorName = "new-api-usage"
