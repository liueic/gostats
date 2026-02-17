# gostats

一个用 Go 实现的轻量后端，用于在 Astro/Hexo 这类无后端博客中展示实时统计数据。

参考项目: [spencerwooo/substats](https://github.com/spencerwooo/substats)

## Features

- `GET /stats/:source/:key` 单指标接口（兼容 substats 风格）
- `GET /stats.json` 标准化数组接口（适合前端一次性拉取）
- 支持 GitHub followers
- 支持 Steam 游戏总数和总游戏时长
- 支持 Spotify 当前播放/最近播放回退、收藏歌曲总数
- 内置 Spotify OAuth 首次授权流程（服务端自动换取并持久化 refresh token）
- 内置短 TTL 内存缓存，降低外部 API 限流风险

## Endpoints

### 1) 单指标

```txt
GET /stats/github/:username
GET /stats/steamgames/:steamid_or_vanity
GET /stats/steamtime/:steamid_or_vanity
GET /stats/spotifyplaying/:key
GET /stats/spotifysaved/:key
GET /spotify/auth/start
GET /spotify/auth/callback
GET /spotify/auth/status
```

`spotify` 的 `:key` 当前仅用于占位和缓存隔离，建议传 `me`。

返回示例:

```json
{
  "source": "github",
  "key": "spencerwooo",
  "metric": "followers",
  "label": "GitHub Followers",
  "failed": false,
  "count": 1234,
  "unit": "followers",
  "updatedAt": "2026-02-17T03:34:00Z"
}
```

### 2) 标准化数组

```txt
GET /stats.json?github=:username&steam=:steamid_or_vanity&spotify=:key
```

返回示例:

```json
[
  {
    "source": "github",
    "key": "spencerwooo",
    "metric": "followers",
    "label": "GitHub Followers",
    "failed": false,
    "count": 1234,
    "unit": "followers",
    "updatedAt": "2026-02-17T03:34:00Z"
  },
  {
    "source": "steamgames",
    "key": "76561198000000000",
    "metric": "games",
    "label": "Steam Games",
    "failed": false,
    "count": 256,
    "unit": "games",
    "updatedAt": "2026-02-17T03:34:00Z"
  },
  {
    "source": "steamtime",
    "key": "76561198000000000",
    "metric": "playtime",
    "label": "Steam Playtime",
    "failed": false,
    "count": 1532.4,
    "unit": "hours",
    "updatedAt": "2026-02-17T03:34:00Z"
  },
  {
    "source": "spotifyplaying",
    "key": "me",
    "metric": "status",
    "label": "Spotify Now Playing",
    "failed": false,
    "count": "Song Name - Artist Name",
    "unit": "track",
    "data": {
      "isPlaying": true,
      "trackName": "Song Name",
      "artists": ["Artist Name"],
      "albumImage": "https://i.scdn.co/image/xxx",
      "progressMs": 12345,
      "trackUrl": "https://open.spotify.com/track/xxx",
      "fromRecent": false
    },
    "updatedAt": "2026-02-17T03:34:00Z"
  },
  {
    "source": "spotifysaved",
    "key": "me",
    "metric": "saved_tracks",
    "label": "Spotify Saved Tracks",
    "failed": false,
    "count": 999,
    "unit": "tracks",
    "updatedAt": "2026-02-17T03:34:00Z"
  }
]
```

## Run

```bash
cp config.example.yml config.yml
# 编辑 config.yml 填入你的密钥
go run ./cmd/gostats
```

程序默认会自动读取当前目录的 `config.yml`。  
同时支持环境变量覆盖（优先级: `环境变量 > config.yml > 默认值`）。
`config.yml` 中也支持 `${ENV_NAME}` 形式的环境变量占位。

配置模板见 `config.example.yml`。

可用环境变量:

- `CONFIG_FILE` (默认 `./config.yml`)
- `PORT` (默认 `8080`)
- `TRUST_PROXY_HEADERS` (默认 `false`。仅当部署在受信反向代理后时设为 `true`)
- `SERVER_READ_HEADER_TIMEOUT` (默认 `5s`)
- `SERVER_READ_TIMEOUT` (默认 `15s`)
- `SERVER_WRITE_TIMEOUT` (默认 `15s`)
- `SERVER_IDLE_TIMEOUT` (默认 `60s`)
- `CACHE_TTL` (默认 `5m`)
- `HTTP_TIMEOUT` (默认 `10s`)
- `CORS_ALLOWED_ORIGINS` (可选。逗号分隔白名单，例如 `https://blog.example.com,https://www.blog.example.com`。默认空=拒绝跨域浏览器访问)
- `GITHUB_TOKEN` (可选，提升 GitHub API 限额)
- `STEAM_API_KEY` (Steam 功能必需)
- `SPOTIFY_CLIENT_ID` (Spotify 必需)
- `SPOTIFY_CLIENT_SECRET` (Spotify 必需)
- `SPOTIFY_REDIRECT_URI` (可选。默认自动推断为 `{scheme}://{host}/spotify/auth/callback`)
- `SPOTIFY_OAUTH_SCOPES` (可选。默认 `user-read-currently-playing user-read-recently-played user-library-read`)
- `SPOTIFY_REFRESH_TOKEN` (可选，长效 Token。可首次为空，后续由服务自动获取)
- `SPOTIFY_REFRESH_TOKEN_FILE` (可选但强烈推荐。用于持久化 refresh token 轮换结果)
- `SPOTIFY_REFRESH_TOKEN_PERSIST_CMD` (可选，token 轮换时自动执行命令写回 Secret Manager，不替代初始 token)

公网部署建议:

- 生产环境请配置 `cors.allowed_origins` 或 `CORS_ALLOWED_ORIGINS`，避免使用 `*`。
- 若服务直连公网（无可信反代），保持 `trust_proxy_headers=false`。
- 若在 Nginx/Caddy/Ingress 后面，再开启 `trust_proxy_headers=true`，并确保代理会覆盖/清洗 `X-Forwarded-*` 请求头。

## 凭证申请（用于测试）

### 1) GitHub Token（可选）

> 仅查询公开用户 follower 时，不配 `GITHUB_TOKEN` 也能用；配置后可提高 API 限额。

1. 打开 GitHub Token 页面（Fine-grained）:
   - <https://github.com/settings/personal-access-tokens/new>
2. 选择你的账号作为 `Resource owner`，设置过期时间。
3. 这个项目只调用公开用户接口（`GET /users/:username`），通常不需要额外权限。
4. 生成后复制 token，配置到环境变量:

```bash
export GITHUB_TOKEN="ghp_xxx"
```

或写入 `config.yml`:

```yaml
github:
  token: "ghp_xxx"
```

### 2) Steam API Key（必需）

1. 用你的 Steam 账号登录后打开:
   - <https://steamcommunity.com/dev/apikey>
2. `Domain Name` 填你的站点域名；本地测试可先填 `localhost`。
3. 同意条款后生成 `Key`，配置到环境变量:

```bash
export STEAM_API_KEY="xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

或写入 `config.yml`:

```yaml
steam:
  api_key: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
```

注意:
- 如果要拿到完整游戏数据，你的 Steam 个人资料和游戏详情需要是公开可见，否则可能返回空或不完整数据。

### 3) Spotify 凭证（必需）

你至少需要 2 个值：`SPOTIFY_CLIENT_ID`、`SPOTIFY_CLIENT_SECRET`。  
`SPOTIFY_REFRESH_TOKEN` 可以留空，服务支持首次授权时自动获取并保存。

1. 打开 Spotify Developer Dashboard 创建应用:
   - <https://developer.spotify.com/dashboard>
2. 在应用设置中添加 Redirect URI（必须和服务回调一致），例如:
   - `https://your-domain.example/spotify/auth/callback`
   - 本地服务端模式请用: `http://127.0.0.1:8080/spotify/auth/callback`
   - `http://127.0.0.1:8787/callback` 仅用于 `go run ./cmd/spotify-auth` 单独授权工具，不用于服务端 `/spotify/auth/start`
3. 在 `config.yml` 填好:

```yaml
spotify:
  client_id: "your_client_id"
  client_secret: "your_client_secret"
  redirect_uri: "https://your-domain.example/spotify/auth/callback"
  refresh_token_file: "./data/spotify_refresh_token"

cors:
  allowed_origins: "https://your-blog.example"
```

4. 启动服务后，打开一次授权入口:

```bash
https://your-domain.example/spotify/auth/start
```

5. 授权完成后，服务会自动:

- 交换 `refresh_token`
- 验证该 token 可直接刷新
- 写入 `refresh_token_file`（如果配置）
- 后续自动轮换并持久化，无需人工管理

可用以下接口检查状态:

```bash
curl "https://your-domain.example/spotify/auth/status"
```

返回 `hasRefreshToken: true` 表示首次授权已完成。

备用方式:
- 仍可使用 `./scripts/spotify-auth.sh` 或 `go run ./cmd/spotify-auth` 在本地完成授权后再部署。

## 本地测试（推荐）

1. `cp config.example.yml config.yml`
2. 编辑 `config.yml` 填入 key
3. 启动:

```bash
go run ./cmd/gostats
```

另开一个终端测试:

```bash
curl "http://127.0.0.1:8080/stats.json?github=spencerwooo&steam=76561198000000000&spotify=me"
```

## Docker

推荐直接使用预构建镜像（GHCR）+ `docker-compose.yml`:

```bash
# 默认使用: ghcr.io/liueic/gostats:latest
docker compose pull
docker compose up -d
```

可选环境变量（用于覆盖 compose 默认值）:

```bash
export GOSTATS_IMAGE="ghcr.io/<owner>/<repo>:latest"
export CORS_ALLOWED_ORIGINS="https://your-blog.example"
export TRUST_PROXY_HEADERS="true"
```

本地自行构建镜像（可选）:

```bash
docker build -t gostats:local .
```

运行容器:

```bash
docker run --rm -p 8080:8080 \
  -v "$(pwd)/config.yml:/config.yml:ro" \
  -v "$(pwd)/data:/data" \
  -e CORS_ALLOWED_ORIGINS='https://your-blog.example' \
  -e TRUST_PROXY_HEADERS=true \
  -e SPOTIFY_REFRESH_TOKEN_FILE=/data/spotify_refresh_token \
  -e SPOTIFY_REFRESH_TOKEN_PERSIST_CMD='your_secret_manager_command' \
  gostats:local
```

说明:
- 推荐把大部分配置写在 `config.yml`，容器只注入少量运行期覆盖参数。
- 镜像默认 `CONFIG_FILE=/config.yml`，因此只挂载 `config.yml` 就会自动读取。
- 服务启动后会优先读取 `SPOTIFY_REFRESH_TOKEN_FILE` 中的 token。
- 当 Spotify 返回新的 refresh token（轮换）时，会自动原子写回该文件。
- 如果设置了 `SPOTIFY_REFRESH_TOKEN_PERSIST_CMD`，每次轮换都会执行该命令，并通过环境变量注入 `SPOTIFY_REFRESH_TOKEN`。
- 容器重启后，只要挂载目录还在，就能继续用最新 token 自动刷新。

### Secret Manager 自动写回示例

`SPOTIFY_REFRESH_TOKEN_PERSIST_CMD` 会在 token 轮换时执行一次，你可以用它对接任意 Secret Manager。

AWS Secrets Manager 示例:

```bash
export SPOTIFY_REFRESH_TOKEN_PERSIST_CMD='aws secretsmanager put-secret-value \
  --secret-id prod/gostats/spotify-refresh-token \
  --secret-string "$SPOTIFY_REFRESH_TOKEN"'
```

1Password CLI 示例:

```bash
export SPOTIFY_REFRESH_TOKEN_PERSIST_CMD='op item edit "gostats-spotify" "password=$SPOTIFY_REFRESH_TOKEN"'
```

## GitHub CI/CD

- CI: `.github/workflows/ci.yml`
  - 在 `push`/`pull_request` 时执行 `gofmt`、`go vet`、`go test`、`go build`
- CD: `.github/workflows/cd.yml`
  - 在 `main` 分支 push、`v*` tag push 或手动触发时
  - 自动构建多架构镜像并推送到 `ghcr.io/<owner>/<repo>`

拉取镜像示例:

```bash
docker pull ghcr.io/<owner>/<repo>:latest
```

## Hexo/Astro 前端调用示例

```js
const res = await fetch("https://your-domain.example/stats.json?github=spencerwooo&steam=76561198000000000&spotify=me");
const stats = await res.json();

for (const item of stats) {
  if (item.failed) continue;
  console.log(item.label, item.count, item.unit);
}
```
