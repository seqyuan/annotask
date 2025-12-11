# 全局数据库并发访问分析

## 当前更新机制

### 1. 数据库连接方式
- 每个 `annotask` 进程独立打开全局数据库连接
- 使用 `sql.Open("sqlite3", dbPath)` 打开连接
- **没有设置任何连接参数**（如 `busy_timeout`、`WAL` 模式等）

### 2. 更新频率
- `MonitorTaskStatus` 函数每秒执行一次更新（`ticker := time.NewTicker(1 * time.Second)`）
- 每个运行中的 `annotask` 进程都会每秒更新一次全局数据库
- 更新操作：`UpdateGlobalTaskRecord` → `UPDATE ... WHERE` → 如果没更新到则 `INSERT`

### 3. 更新逻辑
```go
// UpdateGlobalTaskRecord 的实现
1. 先尝试 UPDATE tasks SET ... WHERE usrID=? AND project=? AND module=? AND starttime=?
2. 检查 RowsAffected()
3. 如果为 0，则执行 INSERT
```

### 4. 错误处理
- 更新失败时只记录日志：`log.Printf("Error updating global DB: %v", err)`
- **没有重试机制**
- **不会中断任务执行**

## 潜在并发问题

### ⚠️ 问题 1: SQLite 文件锁竞争

**场景**：
- 10 个用户同时运行 `annotask`
- 每个进程每秒更新一次全局数据库
- 每秒可能有 10+ 次并发写入操作

**SQLite 的锁机制**：
- SQLite 使用文件锁来保证并发安全
- 默认情况下，写入操作需要获取 `EXCLUSIVE` 锁
- 如果另一个进程正在写入，会返回 `SQLITE_BUSY` 错误
- **当前代码没有设置 `busy_timeout`**，遇到锁会立即失败

**影响**：
- 高并发时，大量更新操作会失败
- 虽然不会中断任务执行，但全局数据库的状态可能不准确

### ⚠️ 问题 2: UPDATE + INSERT 竞态条件

**场景**：
- 两个进程同时启动相同的任务（相同的 usrID、project、module、starttime）
- 进程 A 执行 `UPDATE`，发现没有记录（RowsAffected = 0）
- 进程 B 也执行 `UPDATE`，也发现没有记录
- 进程 A 执行 `INSERT`
- 进程 B 也执行 `INSERT` → **违反 UNIQUE 约束**

**当前保护**：
- 表中有 `UNIQUE(usrID, project, module, starttime)` 约束
- 如果两个进程同时 INSERT，第二个会失败并返回错误
- 但错误只被记录，不会重试 UPDATE

**影响**：
- 可能导致任务记录丢失或不一致

### ⚠️ 问题 3: 没有事务保护

**当前实现**：
- `UPDATE` 和 `INSERT` 是分开执行的，不是原子操作
- 没有使用事务来保证原子性

**影响**：
- 在 UPDATE 和 INSERT 之间，其他进程可能插入记录
- 可能导致数据不一致

### ⚠️ 问题 4: 更新频率过高

**当前情况**：
- 每个进程每秒更新一次
- 如果有 100 个进程同时运行，每秒有 100 次更新操作

**影响**：
- 增加锁竞争的概率
- 增加数据库 I/O 压力
- 可能影响性能

## 风险评估

### 🔴 高风险场景
1. **大量用户同时投递任务**
   - 如果 50+ 用户同时运行 `annotask`
   - 每秒 50+ 次并发写入
   - 锁竞争严重，大量更新失败

2. **相同任务重复启动**
   - 两个进程同时启动相同的任务
   - INSERT 竞态条件可能导致记录丢失

### 🟡 中等风险场景
1. **长时间运行的任务**
   - 任务运行数小时，持续更新全局数据库
   - 增加锁竞争的时间窗口

2. **网络文件系统（NFS）**
   - 如果全局数据库在 NFS 上
   - SQLite 的文件锁在 NFS 上不可靠
   - 可能导致数据损坏

### 🟢 低风险场景
1. **少量用户（< 10）**
   - 锁竞争概率较低
   - 偶尔的更新失败影响不大

2. **本地文件系统**
   - SQLite 文件锁在本地文件系统上可靠
   - 性能较好

## 改进建议

### 1. 启用 WAL 模式（Write-Ahead Logging）
**优点**：
- 支持多个读取器和一个写入器并发
- 减少锁竞争
- 提高并发性能

**实现**：
```go
conn, err := sql.Open("sqlite3", dbPath+"?_journal_mode=WAL")
```

### 2. 设置 busy_timeout
**优点**：
- 遇到锁时自动重试，而不是立即失败
- 减少更新失败的概率

**实现**：
```go
conn, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=5000")
// 或者
conn.Exec("PRAGMA busy_timeout = 5000")
```

### 3. 使用事务保护 UPDATE + INSERT
**优点**：
- 保证原子性
- 减少竞态条件

**实现**：
```go
tx, err := globalDB.Db.Begin()
// UPDATE
// 如果失败，INSERT
tx.Commit()
```

### 4. 使用 INSERT OR REPLACE / INSERT OR IGNORE
**优点**：
- 简化逻辑
- 减少竞态条件

**实现**：
```go
INSERT OR REPLACE INTO tasks(...) VALUES(...)
```

### 5. 降低更新频率
**优点**：
- 减少锁竞争
- 降低数据库压力

**实现**：
- 将更新频率从 1 秒改为 5 秒或 10 秒
- 或者只在状态变化时更新

### 6. 添加重试机制
**优点**：
- 提高更新成功率
- 更好的容错性

**实现**：
```go
func UpdateGlobalTaskRecordWithRetry(...) {
    for i := 0; i < 3; i++ {
        err := UpdateGlobalTaskRecord(...)
        if err == nil {
            return nil
        }
        if isRetryableError(err) {
            time.Sleep(time.Duration(i+1) * 100 * time.Millisecond)
            continue
        }
        return err
    }
    return err
}
```

## 结论

**当前实现存在并发风险**，特别是在以下情况：
1. 大量用户（> 20）同时使用
2. 相同任务重复启动
3. 数据库在 NFS 上

**建议优先实施**：
1. ✅ 启用 WAL 模式
2. ✅ 设置 busy_timeout
3. ✅ 使用事务保护 UPDATE + INSERT
4. ⚠️ 考虑降低更新频率（需要权衡实时性）

**对于当前使用场景**：
- 如果用户数量较少（< 10），风险较低
- 如果用户数量较多（> 20），建议尽快实施改进措施

