# RouterHub

单用户 LLM API 聚合网关。Go 后端 + React 管理台，打包为单一可执行文件。

- 后端：Go 1.23 + `chi/v5` + `modernc.org/sqlite`（纯 Go）
- 前端：React 18 + TypeScript + Vite + Tailwind CSS + TanStack Query
- 前端产物由 `embed.FS` 嵌入二进制

## 特性

- **三协议网关**：OpenAI Chat Completions、OpenAI Responses、Anthropic Messages
- **协议互转**：客户端和上游可用任意组合，网关自动转换请求/响应/SSE
- **模型路由**：模型别名 → `model_prefix/model_name` → 原始模型名
- **实时仪表盘**：请求量、成功率、TTFT、Token 使用（缓存读/写分列），SSE 推送
- **请求日志**：模型、渠道、API 密钥、缓存命中率、首字延迟等，可搜索
- **API 密钥**：后端生成 `rh-` 前缀密钥
- **i18n**：简体中文 / English；主题浅色 / 深色 / 跟随系统
- **时区**：数据库统一 UTC，前端按用户偏好渲染

## 快速开始

需要 Go 1.23+ 与 Node.js 20+。

```powershell
# 一键构建（Windows）
powershell -File scripts\build.ps1

# 配置（可选，只有 host / port 两项）
Copy-Item config.example.yaml config.yaml

# 启动
.\routerhub.exe
```

浏览器打开 `http://127.0.0.1:8080`，首次访问会进入 Setup 向导创建管理员账号。

## 使用流程

1. 登录管理台
2. **提供商** 页面添加 Provider（类型、Base URL 到 `/v1`、API Key）
3. 手动加模型或点"从提供商获取"批量导入
4. **API 密钥** 页面创建网关密钥
5. 客户端请求本地网关：

```bash
curl http://127.0.0.1:8080/v1/chat/completions \
  -H "Authorization: Bearer rh-..." \
  -H "Content-Type: application/json" \
  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "hello"}]}'
```

## 项目结构

```
cmd/routerhub/       入口
internal/
  admin/             管理 API
  gateway/           LLM 网关（鉴权、模型选择、代理、SSE、usage 解析）
  convert/           三种协议互转
  storage/           SQLite + 迁移
  server/            chi 路由
  config/            config.yaml 加载
  events/            内存事件总线（驱动 SSE）
  webui/             embed.FS 打包前端
  protocol/          协议识别
  providerapi/       后台拉取上游模型列表
scripts/build.ps1    Windows 一键构建
web/                 前端源码
data/routerhub.db    SQLite 数据文件（运行期创建）
```

## 开发

```powershell
# 前端
cd web; npm install; npm run build

# 后端
go build -o routerhub.exe ./cmd/routerhub

# 注入版本号（取自 web/package.json，单一来源）与构建日期（显示在设置页）
$version = (Get-Content -Raw web/package.json | ConvertFrom-Json).version
$now = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
go build -ldflags "-X github.com/lovelytoaster94/routerhub/internal/admin.AppVersion=$version -X github.com/lovelytoaster94/routerhub/internal/admin.BuildDate=$now" -o routerhub.exe ./cmd/routerhub

# 测试
go test ./...
```

## 数据

- SQLite 固定路径 `./data/routerhub.db`
- 启动时自动迁移
- 忘记密码：`Remove-Item data\routerhub.db*`（会清空所有数据）
