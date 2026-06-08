# Go 项目 AI 审查规则

## 安全（CRITICAL）

1. **硬编码密钥**：禁止在源码中硬编码 API Key、Token、密码。使用环境变量或 os.Getenv + 配置中心。
2. **SQL 注入**：禁止 `fmt.Sprintf` 拼接 SQL。使用 GORM 参数化查询或 `?` 占位符。
3. **命令注入**：禁止用户输入直接传入 `exec.Command`。使用 `exec.Command("cmd", arg1, arg2)` 参数数组形式。
4. **模板注入**：`html/template` 会自动转义；禁止用 `text/template` 渲染用户输入到 HTML。
5. **缺少超时**：`http.Client`、`sql.DB`、Redis 客户端必须设置 Timeout。无超时导致 goroutine 泄漏和服务雪崩。
6. **不安全反序列化**：`json.Unmarshal` 到 `interface{}` 后未做类型断言检查，可能 panic。

## 错误处理（HIGH）

7. **吞错误（`_ = err` / 空 `if err != nil {}`）**：错误必须处理 `log + return` 或 `wrap + propagate`。
8. **panic 未 recover**：goroutine 内必须 defer recover，否则单个 panic 会崩整个进程。
9. **日志泄露敏感数据**：禁止 `log.Print(user.Password)` 或 `fmt.Printf("%+v", req)` 打印完整请求体。
10. **输入校验缺失**：API handler 的 path/query/body 参数必须校验类型、长度、范围。使用 `binding:"required"` 等标签。

## 并发安全（HIGH）

11. **未加锁的共享变量**：多个 goroutine 读写同一变量必须使用 `sync.Mutex` 或 `sync.RWMutex`。
12. **channel 未关闭导致 goroutine 泄漏**：生产者 goroutine 退出时必须 close channel，消费者用 `for range`。
13. **WaitGroup 使用错误**：`wg.Add(1)` 必须在 goroutine 外部调用，否则竞态。

## 性能（MEDIUM）

14. **N+1 查询**：循环内调用 DB/Redis，GORM 缺 `Preload` 导致 N+1。
15. **defer 在循环内**：`for { defer f.Close() }` 导致资源延迟释放和内存堆积。
16. **不必要的内存分配**：能用 `var buf [N]byte` 栈分配的不用 `make([]byte, N)` 堆分配。
17. **sync.Pool 未使用**：高频创建/销毁的对象（如 buffer）应使用 sync.Pool 复用。

## 代码质量（MEDIUM）

18. **魔法数字**：业务逻辑中的数字字面量必须命名常量。`10*time.Second` 应为 `const DefaultTimeout = 10 * time.Second`。
19. **未处理的 context 取消**：`ctx` 传入了但函数内从未检查 `ctx.Done()`。
20. **interface{} 滥用**：能用泛型/具体类型的不用 `interface{}` / `any`。
21. **重复代码**：跨文件的相同逻辑块应提取为函数。相同错误处理模式应封装。

## 数据库（HIGH）

22. **NOT NULL 无默认值**：新加 NOT NULL 列必须有 DEFAULT，或分多步迁移。
23. **迁移未检查表锁**：MySQL ALTER TABLE 可能导致锁表，大表迁移需说明方案。
24. **GORM 零值更新陷阱**：`Updates` 不会更新零值字段（`0`, `""`, `false`），应用 `Select` 指定或使用 map。

## 反面规则（不要查）

- 代码格式（gofmt 已处理）
- import 分组顺序（goimports 已处理）
- 变量命名风格偏好（如 `c` vs `ctx`）
- 无具体理由的模式建议
- 注释语言选择（中英文均可）
