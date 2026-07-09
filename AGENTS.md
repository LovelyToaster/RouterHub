# AGENTS.md

## 定位

Go 后端 + React 管理台的 LLM API 聚合网关。

## 目录

```
cmd/routerhub/         入口
internal/
  admin/               管理 API（登录、Provider、Key、Settings、Stats、RequestLogs、SystemInfo）
  gateway/             LLM 网关（鉴权、模型选择、代理、SSE、usage 解析）
  convert/             OpenAI Chat / OpenAI Responses / Anthropic Messages 三向互转
  storage/             SQLite + 迁移
  server/              chi 路由
  config/              config.yaml 加载
  events/              内存事件总线（SSE）
  webui/               embed.FS 打包前端
  protocol/            协议识别
  providerapi/         拉取上游模型列表
scripts/build.ps1      Windows 构建脚本
web/                   前端源码
data/routerhub.db      SQLite（运行期创建）
```

## 技术栈

- 后端：Go 1.23、`chi/v5`、`modernc.org/sqlite`（纯 Go）、`uuid`、`yaml.v3`；标准库 `net/http` + `encoding/json`
- 前端：React 18 + TypeScript 5 + Vite 6 + Tailwind CSS 3 + TanStack Query 5 + react-router 6 + i18next + recharts + lucide-react
- 实时：EventSource → `/api/stats/summary/stream`，服务端 200ms 节流 + 15s 心跳

## 命令

```powershell
# 一键构建（Windows）
powershell -File scripts\build.ps1

# 分开构建
cd web; npm install; npm run build; cd ..
go build -o routerhub.exe ./cmd/routerhub

# 注入 BuildDate（RFC3339 UTC）
$now = (Get-Date).ToUniversalTime().ToString("yyyy-MM-ddTHH:mm:ssZ")
go build -ldflags "-X github.com/lovelytoaster94/routerhub/internal/admin.BuildDate=$now" -o routerhub.exe ./cmd/routerhub

# 测试
go test ./...
```

## API 端点

- Admin（Bearer session token）：`/api/auth/*`、`/api/providers`、`/api/model-aliases`、`/api/gateway-keys`、`/api/settings`、`/api/stats/summary`、`/api/stats/summary/stream`（SSE）、`/api/request-logs`、`/api/request-logs/stream`（SSE）、`/api/system/info`
- 网关（`Authorization: Bearer rh-...`）：`/v1/chat/completions`、`/v1/responses`、`/v1/messages`
- SPA 静态资源走 `internal/webui/dist`，`chi.NotFound` 兜底

## 数据模型

- `providers`：`name / type / base_url / api_key / enabled`
- `provider_models`：`provider_id / model_name / model_prefix / enabled`
- `model_aliases`：`alias / provider_id / target_model`
- `gateway_api_keys`：`crypto/rand` 生成 `rh-` + 43 字符 base64 URL-safe；`name` 必填
- `admin_users`：单条；`timezone` 空触发浏览器 IANA 回写
- `admin_sessions`：Bearer Token
- `request_logs`：含 `client_ip / gateway_api_key_name / cache_write_tokens / inbound_protocol`（`inbound_protocol` 为客户端入站协议；跨协议转换时与上游 `provider_type` 不同）

## 修改流程

- **新增协议 / Provider 类型**：`internal/convert/` 三向互转 → `internal/gateway/proxy.go` 的 `parseUsageFromResponse` / stream 分支 → `web/src/pages/ProvidersPage.tsx` 类型枚举 + i18n
- **改 schema**：`internal/storage/migrate.go` 追加 phase → `models.go` 与相关 CRUD → 调用点同步
- **新增页面**：`web/src/pages/*Page.tsx` + 路由 `web/src/App.tsx` + 侧栏 `Layout.tsx` + i18n
- **共享工具**：`web/src/lib/`（`format.ts` / `timezone.ts`），避免各页面重复
