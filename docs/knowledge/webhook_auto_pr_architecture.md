# Webhook Auto PR - 技术架构

## 系统架构

```
┌─────────────────────────────────────────────────────────────────┐
│                         GitHub                                   │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐            │
│  │  Issue  │  │   PR    │  │ Comment │  │  Repo   │            │
│  └────┬────┘  └────┬────┘  └────┬────┘  └────┬────┘            │
│       │            │            │            │                   │
│       └────────────┴────────────┴────────────┘                   │
│                         │                                        │
│                    Webhook Event                                 │
└─────────────────────────┬───────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────┐
│                      git-sonic 服务                              │
│                                                                  │
│  ┌──────────────┐    ┌──────────────┐    ┌──────────────┐      │
│  │    Server    │───►│    Queue     │───►│   Worker     │      │
│  │  (HTTP 接收) │    │   (任务队列)  │    │  (并发处理)   │      │
│  └──────────────┘    └──────────────┘    └──────┬───────┘      │
│         │                                        │               │
│         │                                        ▼               │
│  ┌──────┴──────┐                         ┌──────────────┐      │
│  │  Allowlist  │                         │    Engine    │      │
│  │  (IP 过滤)  │                         │  (工作流引擎) │      │
│  └─────────────┘                         └──────┬───────┘      │
│                                                  │               │
│                    ┌─────────────────────────────┼───────┐      │
│                    │                             │       │      │
│                    ▼                             ▼       ▼      │
│             ┌──────────┐                  ┌─────────┐ ┌─────┐  │
│             │ GitClient│                  │LLMRunner│ │GitHub│  │
│             │(git操作) │                  │(LLM调用)│ │Client│  │
│             └──────────┘                  └─────────┘ └─────┘  │
└─────────────────────────────────────────────────────────────────┘
```

---

## 核心组件

### 1. Server (`pkg/server/`)

HTTP 服务器，接收 GitHub webhook：

```go
type Server struct {
    cfg       config.Config
    allowlist allowlist.Allowlist
    queue     *queue.Queue
    logger    *logging.Logger
}
```

**职责**：
- 验证请求方法 (POST)
- IP 白名单检查
- 解析 webhook payload
- 将事件加入队列

### 2. Queue (`pkg/queue/`)

并发任务队列：

```go
type Queue struct {
    jobs    chan Job
    handler Handler
    wg      sync.WaitGroup
}
```

**职责**：
- 管理 worker 池
- 异步处理任务
- 优雅关闭

### 3. Engine (`pkg/workflow/`)

工作流引擎，核心处理逻辑：

```go
type Engine struct {
    cfg    config.Config
    gh     GitHubClient
    git    GitClient
    llm    LLMRunner
    now    func() time.Time
    logger *logging.Logger
}
```

**职责**：
- Issue 处理工作流
- PR 优化工作流
- 标签状态管理
- 错误处理和日志

### 4. GitClient (`pkg/gitutil/`)

Git 操作封装：

```go
type Client struct {
    GitBinary string
}

// 主要方法
func (c Client) Clone(ctx, repoURL, dir string) error
func (c Client) CheckoutBranch(ctx, dir, branch, base string) error
func (c Client) CommitAll(ctx, dir, message string) error  // 排除自动化产物
func (c Client) Push(ctx, dir, branch string) error
func (c Client) HasChanges(ctx, dir string) (bool, error)  // 排除自动化产物
func (c Client) ApplyPatch(ctx, dir, patch string) error
```

**排除的文件** (安全机制，当前已不太需要因为产物在 `outputs/` 目录)：
```go
var ExcludedFiles = map[string]bool{
    "context.json":         true,
    "repo_instructions.md": true,
    "prompt.md":            true,
    "llm_response.json":    true,
    "llm_output.json":      true,
    "run.log":              true,
}
```

### 5. LLMRunner (`pkg/llm/`)

LLM 调用接口：

```go
type Runner interface {
    Run(ctx context.Context, req Request, workDir string) (RunResult, error)
}

// 两种实现
type CommandRunner struct { ... }  // CLI 模式
type APIRunner struct { ... }      // API 模式
```

**LLM 输出格式**：
```go
type Response struct {
    Decision         Decision          `json:"decision"`          // proceed/needs_info/stop
    NeedsInfoComment string            `json:"needs_info_comment"`
    CommitMessage    string            `json:"commit_message"`
    PRTitle          string            `json:"pr_title"`
    PRBody           string            `json:"pr_body"`
    Files            map[string]string `json:"files"`             // 文件路径 -> 完整内容
    Patch            string            `json:"patch"`             // 已弃用
    Summary          string            `json:"summary"`
}
```

### 6. Logger (`pkg/logging/`)

结构化日志和工作流追踪：

```go
type Logger struct {
    *slog.Logger
    workflow  string
    startTime time.Time
    stepNum   int
}

// 工作流追踪
func (l *Logger) StartWorkflow(name string, attrs ...any) *Logger
func (l *Logger) Step(stepName string, attrs ...any) func(error)
func (l *Logger) EndWorkflow(err error)
func (l *Logger) WrapError(step, op string, err error) error
```

**错误类型**：
```go
type WorkflowError struct {
    Workflow string
    Step     string
    StepNum  int
    Op       string
    Err      error
    Stack    string  // 调用栈
}
```

---

## 数据流

### Webhook 接收流程

```
GitHub POST /webhook
       │
       ▼
┌──────────────────┐
│ Server.handleWebhook │
│  1. 验证 Method  │
│  2. IP 白名单    │
│  3. 解析 Event   │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  Queue.Enqueue   │
│  (非阻塞入队)    │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  Worker Goroutine │
│  1. 取出 Job     │
│  2. 调用 Handler │
└────────┬─────────┘
         │
         ▼
┌──────────────────┐
│  Engine.Handle*  │
│  (工作流处理)    │
└──────────────────┘
```

### Issue 处理流程

```
HandleIssueLabel(event)
       │
       ├─ 1. 验证事件类型和标签
       │
       ├─ 2. splitFullName(repo) → owner, repo
       │
       ├─ 3. gh.GetIssue() → issue details
       │
       ├─ 4. gh.ListIssueComments() → comments
       │
       ├─ 5. prepareWorkspace() → workDir
       │      └─ git.Clone()
       │
       ├─ 6. git.SetRemoteAuth()
       │
       ├─ 7. gh.GetRepo() → defaultBranch
       │
       ├─ 8. git.CheckoutBranch(llm/issue-{n}-{timestamp})
       │
       ├─ 9. gh.SetIssueLabels(ai-in-progress)
       │
       ├─ 10. preparePrompt() → context.json, prompt.md
       │
       ├─ 11. llm.Run() → Response
       │
       ├─ 12. 检查 decision
       │       ├─ proceed → 继续
       │       ├─ needs_info → 设置标签,评论,返回
       │       └─ stop → 评论,返回
       │
       ├─ 13. applyChanges() → 写入文件
       │
       ├─ 14. git.HasChanges() → 检查是否有更改
       │
       ├─ 15. git.CommitAll() → 提交(排除产物)
       │
       ├─ 16. git.Push()
       │
       ├─ 17. gh.CreatePR()
       │
       ├─ 18. gh.AddAssignees() (可选)
       │
       ├─ 19. gh.SetIssueLabels(ai-done)
       │
       └─ 20. gh.CreateIssueComment(PR link)
```

---

## 文件结构

```
git_sonic/
├── cmd/
│   └── git-sonic/
│       └── main.go              # 入口点
├── pkg/
│   ├── allowlist/               # IP 白名单
│   │   └── allowlist.go
│   ├── config/                  # 配置加载
│   │   └── config.go
│   ├── github/                  # GitHub API 客户端
│   │   └── client.go
│   ├── gitutil/                 # Git 操作
│   │   └── git.go
│   ├── llm/                     # LLM 调用
│   │   └── llm.go
│   ├── logging/                 # 结构化日志
│   │   └── logger.go
│   ├── queue/                   # 任务队列
│   │   └── queue.go
│   ├── server/                  # HTTP 服务
│   │   └── server.go
│   ├── webhook/                 # Webhook 解析
│   │   └── webhook.go
│   └── workflow/                # 工作流引擎
│       └── engine.go
├── tests/
│   ├── contract/                # 契约测试
│   ├── e2e/                     # 端到端测试
│   ├── integration/             # 集成测试
│   └── unit/                    # 单元测试
├── docs/
│   ├── knowledge/               # 知识文档
│   └── specs/                   # 规格文档
└── workdir/                     # 工作目录 (运行时生成)
```

---

## 工作空间目录结构

每个工作流执行会创建独立的工作空间目录，结构如下：

```
workdir/
└── issue-123-20240115-143022/   # 工作空间根目录 (prefix-timestamp)
    ├── repo/                     # 仓库克隆目录
    │   ├── .git/
    │   ├── src/
    │   ├── README.md
    │   ├── AGENT.md             # 仓库指令文件 (可选)
    │   └── ...                  # 仓库文件
    └── outputs/                  # 中间产物目录
        ├── context.json          # 上下文信息
        ├── repo_instructions.md  # 仓库指令汇总
        ├── prompt.md             # LLM 提示词
        ├── llm_response.json     # LLM 响应输出路径
        ├── llm_output.json       # LLM 实际输出
        └── run.log               # 运行日志
```

**设计原理**：
- `repo/` 子目录仅包含仓库代码，便于 git 操作
- `outputs/` 子目录存放自动化产物，不会被误提交
- 根目录名称保持原有命名规则 (`prefix-timestamp`)

**相关常量** (`pkg/workflow/engine.go`):
```go
const (
    RepoSubdir    = "repo"     // 仓库子目录
    OutputsSubdir = "outputs"  // 产物子目录
)
```

---

## 接口定义

### GitHubClient

```go
type GitHubClient interface {
    GetIssue(ctx, owner, repo string, number int) (Issue, error)
    ListIssueComments(ctx, owner, repo string, number int) ([]Comment, error)
    CreateIssueComment(ctx, owner, repo string, number int, body string) error
    SetIssueLabels(ctx, owner, repo string, number int, labels []string) error
    CreatePR(ctx, owner, repo string, req PRRequest) (PR, error)
    UpdatePRBody(ctx, owner, repo string, number int, body string) error
    AddAssignees(ctx, owner, repo string, number int, assignees []string) error
    GetRepo(ctx, owner, repo string) (Repo, error)
    GetPR(ctx, owner, repo string, number int) (PR, error)
}
```

### GitClient

```go
type GitClient interface {
    Clone(ctx, repoURL, dir string) error
    CheckoutBranch(ctx, dir, branch, base string) error
    CommitAll(ctx, dir, message string) error
    Push(ctx, dir, branch string) error
    SetRemoteAuth(ctx, dir, token string) error
    ApplyPatch(ctx, dir, patch string) error
    HasChanges(ctx, dir string) (bool, error)
}
```

### LLMRunner

```go
type LLMRunner interface {
    Run(ctx, req Request, workDir string) (RunResult, error)
}
```

---

## 错误处理

### 错误包装

所有工作流错误都通过 `WorkflowError` 包装：

```go
return log.WrapError("step-name", "operation", err)
// 输出: [workflow] step N (step-name) operation: original error
```

### 错误恢复

- LLM 失败：评论错误信息到 issue
- Git 操作失败：返回错误，保留工作目录供调试
- GitHub API 失败：返回错误，不改变 issue 状态

### 错误日志

错误使用 `%+v` 格式化以包含完整栈：

```go
log.Printf("job error: %s err=%+v", summary, err)
```

---

## 性能考虑

### 并发控制

- `MAX_WORKERS` 控制并发处理数
- 队列缓冲区大小 = workers * 4
- 每个 job 独立工作目录，无锁竞争

### 资源管理

- 工作目录不自动清理（保留供调试）
- 建议定期清理旧的 workdir
- LLM 调用有超时控制 (`LLM_TIMEOUT`)

### 优化建议

1. **工作目录清理**：添加定时清理过期目录
2. **缓存仓库**：避免重复克隆同一仓库
3. **限流**：对同一 issue 的并发请求进行限流
