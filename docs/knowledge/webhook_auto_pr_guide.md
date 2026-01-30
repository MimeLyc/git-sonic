# Webhook Auto PR - 使用指南

## 概述

Webhook Auto PR 是一个自动化系统，通过监听 GitHub webhook 事件，使用 LLM 自动处理 issue 并创建 PR。

## 核心功能

1. **Issue 自动处理**：当 issue 被打上触发标签时，自动分析并生成修复代码
2. **PR 自动创建**：将修复代码提交为 Pull Request
3. **状态追踪**：通过标签追踪处理状态
4. **评论通知**：在 issue 上添加评论通知处理进度

---

## 状态机

### 标签状态

系统使用标签来追踪 issue 的处理状态，**每个 issue 上最多只有一个状态标签**：

```
┌─────────────┐
│  (无标签)   │
└──────┬──────┘
       │ 用户添加 ai-ready
       ▼
┌─────────────┐
│  ai-ready   │ ← 触发标签，表示 issue 准备好被自动化处理
└──────┬──────┘
       │ 系统开始处理
       ▼
┌──────────────────┐
│  ai-in-progress  │ ← 正在处理中
└────────┬─────────┘
         │
    ┌────┴────┐
    │         │
    ▼         ▼
┌────────┐  ┌───────────────┐
│ai-done │  │ ai-needs-info │ ← 需要更多信息
└────────┘  └───────┬───────┘
                    │ 用户补充信息后评论
                    ▼
            ┌──────────────────┐
            │  ai-in-progress  │ ← 重新处理
            └──────────────────┘
```

### 状态转换规则

| 当前状态 | 事件 | 新状态 | 移除标签 |
|---------|------|--------|----------|
| 无/任意 | 添加 `ai-ready` | `ai-in-progress` | `ai-ready`, `ai-needs-info`, `ai-done` |
| `ai-in-progress` | LLM 成功 | `ai-done` | `ai-ready`, `ai-in-progress`, `ai-needs-info` |
| `ai-in-progress` | LLM 需要信息 | `ai-needs-info` | `ai-ready`, `ai-in-progress`, `ai-done` |
| `ai-needs-info` | 用户评论 | `ai-in-progress` | `ai-ready`, `ai-needs-info`, `ai-done` |

---

## 工作流程

### Issue 处理流程

```
1. 用户给 issue 添加 ai-ready 标签
2. GitHub 发送 webhook 到服务
3. 服务验证请求并加入队列
4. Worker 开始处理：
   a. 更新标签为 ai-in-progress
   b. 克隆仓库到工作目录
   c. 创建新分支 (llm/issue-{number}-{timestamp})
   d. 准备 LLM prompt
   e. 调用 LLM 分析 issue 并生成修复
   f. 将 LLM 返回的文件内容写入
   g. 提交更改 (排除自动化产物)
   h. 推送到远程
   i. 创建 PR
   j. 更新标签为 ai-done
   k. 在 issue 上评论 PR 链接
```

### 目录结构

每次处理会在 `workdir/` 下创建独立目录：

```
workdir/
└── issue-148-20260124-232515/
    ├── .git/                    # 克隆的仓库
    ├── context.json             # Issue 上下文 (不提交)
    ├── repo_instructions.md     # 仓库说明 (不提交)
    ├── prompt.md                # LLM prompt (不提交)
    ├── llm_output.json          # LLM 原始输出 (不提交)
    ├── run.log                  # 执行日志 (不提交)
    └── [修改的文件...]          # 实际修改的文件 (提交)
```

---

## 配置

### 环境变量

| 变量名 | 默认值 | 说明 |
|--------|--------|------|
| `LISTEN_ADDR` | `:8080` | HTTP 监听地址 |
| `WEBHOOK_PATH` | `/webhook` | Webhook 端点路径 |
| `IP_ALLOWLIST` | (空) | 允许的 IP/CIDR，逗号分隔 |
| `GITHUB_TOKEN` | (必需) | GitHub Token，需要 repo 权限 |
| `REPO_CLONE_BASE` | `./workdir` | 工作目录基础路径 |
| `MAX_WORKERS` | `2` | 并发 worker 数量 |
| `TRIGGER_LABELS` | `ai-ready` | 触发标签，逗号分隔 |
| `IN_PROGRESS_LABEL` | `ai-in-progress` | 处理中标签 |
| `NEEDS_INFO_LABEL` | `ai-needs-info` | 需要信息标签 |
| `DONE_LABEL` | `ai-done` | 完成标签 |
| `LLM_TIMEOUT` | `30m` | LLM 执行超时 |

### LLM 配置 - CLI 模式

```bash
LLM_COMMAND=claude
LLM_ARGS="--model claude-sonnet-4-20250514"
```

### LLM 配置 - API 模式

```bash
LLM_API_BASE_URL=https://api.anthropic.com
LLM_API_KEY=sk-xxx
LLM_API_MODEL=claude-sonnet-4-20250514
LLM_API_PATH=/v1/messages          # 可选
LLM_API_KEY_HEADER=x-api-key       # 可选
LLM_API_KEY_PREFIX=                # 可选
LLM_API_MAX_ATTEMPTS=3             # 可选，重试次数
```

---

## 部署

### 本地运行

```bash
# 设置环境变量
export GITHUB_TOKEN=ghp_xxx
export LLM_COMMAND=claude
export TRIGGER_LABELS=ai-ready

# 运行
go run ./cmd/git-sonic/main.go

# 或构建后运行
go build -o git-sonic ./cmd/git-sonic
./git-sonic
```

### Docker 部署

```dockerfile
FROM golang:1.21-alpine AS builder
WORKDIR /app
COPY . .
RUN go build -o git-sonic ./cmd/git-sonic

FROM alpine:latest
RUN apk add --no-cache git
COPY --from=builder /app/git-sonic /usr/local/bin/
ENTRYPOINT ["git-sonic"]
```

```bash
docker build -t git-sonic .
docker run -d \
  -e GITHUB_TOKEN=ghp_xxx \
  -e LLM_API_BASE_URL=https://api.anthropic.com \
  -e LLM_API_KEY=sk-xxx \
  -e LLM_API_MODEL=claude-sonnet-4-20250514 \
  -p 8080:8080 \
  git-sonic
```

### Kubernetes 部署

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: git-sonic
spec:
  replicas: 1
  selector:
    matchLabels:
      app: git-sonic
  template:
    metadata:
      labels:
        app: git-sonic
    spec:
      containers:
      - name: git-sonic
        image: git-sonic:latest
        ports:
        - containerPort: 8080
        env:
        - name: GITHUB_TOKEN
          valueFrom:
            secretKeyRef:
              name: git-sonic-secrets
              key: github-token
        - name: LLM_API_KEY
          valueFrom:
            secretKeyRef:
              name: git-sonic-secrets
              key: llm-api-key
        - name: LLM_API_BASE_URL
          value: "https://api.anthropic.com"
        - name: LLM_API_MODEL
          value: "claude-sonnet-4-20250514"
        resources:
          requests:
            memory: "256Mi"
            cpu: "100m"
          limits:
            memory: "1Gi"
            cpu: "500m"
---
apiVersion: v1
kind: Service
metadata:
  name: git-sonic
spec:
  selector:
    app: git-sonic
  ports:
  - port: 80
    targetPort: 8080
```

### GitHub Webhook 配置

1. 进入仓库 Settings → Webhooks → Add webhook
2. 配置：
   - Payload URL: `https://your-domain.com/webhook`
   - Content type: `application/json`
   - Secret: (可选)
   - Events: 选择 `Issues`, `Issue comments`, `Pull request review comments`

---

## 使用方法

### 基本使用

1. 确保服务已部署并配置好 webhook
2. 在 GitHub 仓库创建标签：`ai-ready`, `ai-in-progress`, `ai-needs-info`, `ai-done`
3. 在需要自动处理的 issue 上添加 `ai-ready` 标签
4. 等待系统处理，观察标签变化和评论

### 查看处理状态

- `ai-in-progress`：正在处理中
- `ai-needs-info`：LLM 需要更多信息，查看评论了解详情
- `ai-done`：处理完成，查看评论获取 PR 链接

### 重新触发

如果需要重新处理：
1. 移除当前状态标签
2. 重新添加 `ai-ready` 标签

---

## 日志说明

### 日志格式

```
ts="2026-01-24 23:28:12" level=INFO msg="workflow started" workflow=issue-label issue=148 repo=org/repo
ts="2026-01-24 23:28:12" level=INFO msg="step started" workflow=issue-label step=clone-repo step_num=4
ts="2026-01-24 23:28:15" level=INFO msg="step completed" workflow=issue-label step=clone-repo elapsed_ms=3000
```

### 日志字段

| 字段 | 说明 |
|------|------|
| `ts` | 时间戳 |
| `level` | 日志级别 (DEBUG/INFO/WARN/ERROR) |
| `msg` | 日志消息 |
| `workflow` | 工作流名称 |
| `step` | 当前步骤 |
| `step_num` | 步骤编号 |
| `elapsed_ms` | 耗时(毫秒) |
| `error` | 错误信息 |

### 错误日志

错误日志包含完整调用栈：

```
[issue-label] step 14 (push-changes) Push: exit status 1

Stack trace:
  git_sonic/pkg/workflow.(*Engine).handleIssue
    /path/to/engine.go:494
```

---

## 故障排查

详见 [troubleshooting.md](../specs/1_webhook_auto_pr/impl_details/troubleshooting.md)

### 常见问题

1. **Push 失败 (non-fast-forward)**：分支名冲突，已通过时间戳修复
2. **Patch 格式无效**：已改用 files 字段代替 patch
3. **自动化产物被提交**：已在 CommitAll 中排除

### 调试命令

```bash
# 查看最新工作目录
ls -lt workdir/ | head -5

# 查看 LLM 输出
cat workdir/issue-XXX-*/llm_output.json | jq .

# 查看 Git 状态
cd workdir/issue-XXX-* && git status && git log --oneline -5
```
