# Linux 服务器部署

在本仓库目录执行部署，开发分支为 `dev`。以下 `192.168.1.100` 为示例地址，请在私有 `.env` 中设置实际 `MYMIHOMO_LAN_IP`。
只操作 Compose 项目 `mymihomo-dev`，不得操作原 `clash` 容器及它的代理。

## 入口

- 局域网 zashboard：`http://192.168.1.100:9097`
- 本机：`http://127.0.0.1:9097`
- 混合代理：`127.0.0.1:7897` 和 `192.168.1.100:7897`（HTTP/SOCKS5）
- 内核 API：容器内 `127.0.0.1:19097`，不对主机发布
- 旧配置接口 18087 已移除；原 Clash 的 7891/9090 不变

所有页面、管理 API、内核 REST 和 WebSocket 都经过登录网关。使用 `.env` 的 `WEB_CONFIG_SECRET` 登录；未单独配置时回退 `EXTERNAL_SECRET`。不再把内核密钥填到浏览器。

## 构建与更新

新部署可复制 `.env.example` 为 `.env` 并填写密钥；已有部署保留原 `.env`。

旧款 x86 CPU 可使用 compatible 内核；本地 Dockerfile 默认使用 `local/mihomo:v1.19.31-compatible`，需要事先准备该镜像。
不要换成要求 x86-64-v3 的内核。当前构建不升级内核，只更新本项目和 zashboard。

```sh
cd /path/to/mymihomo
# 通过仍在运行的原代理下载前端依赖
HTTPS_PROXY=http://127.0.0.1:7891 HTTP_PROXY=http://127.0.0.1:7891 scripts/build-ui.sh

docker build --network=host \
  --build-arg HTTP_PROXY=http://127.0.0.1:7891 \
  --build-arg HTTPS_PROXY=http://127.0.0.1:7891 \
  -f Dockerfile.local -t local/dqgod-mymihomo:dev .

docker compose -f compose.local.yaml up -d --no-deps --force-recreate mymihomo
docker compose -f compose.local.yaml ps
```

本地 Dockerfile 使用上一步生成的 `web/zashboard/dist`；通用 Dockerfile 会自行构建前端。
升级前备份 `conf/`、`.env` 和旧镜像标签。只重建新实例，原 Clash 持续服务。

## 订阅更新

网页登录后在“订阅”操作，或执行：

```sh
docker exec mymihomo-dev mymihomo update
```

远程订阅更新时下载链接，然后执行覆写、脚本、校验和热加载；本地订阅重新应用本地内容。
切换“使用”以及容器重启均使用已保存的订阅，不重新下载。
定时任务以 `conf/profiles.json` 为准，网页修改立即生效，不依赖旧 cron 环境变量。

“代理更新”的请求从容器发出，`127.0.0.1` 指容器自身。
使用主机原代理时可填 `http://host.docker.internal:7891`（Compose 已配置 host-gateway）。
留空时使用 `.env` 的 `HTTPS_PROXY` / `HTTP_PROXY`；选择直连会明确绕过这些变量。

## 密钥与私有数据

在 zashboard 设置页的“访问与安全”修改访问密码，输入当前密码、新密码及确认密码。保存立即生效，并注销所有已有登录会话和 WebSocket；代理不重启。
网页密码以 bcrypt 摘要存于 `conf/auth.json`，权限 0600。存在该文件时优先使用它，`.env` 的旧 `WEB_CONFIG_SECRET` 不再能登录。修改访问密码不会更改 `EXTERNAL_SECRET`。
忘记密码时，由主机管理员停止新实例，备份并移走 `conf/auth.json`，在 `.env` 设置新的 `WEB_CONFIG_SECRET` 后重新启动新实例；不要操作原 Clash。
修改 `EXTERNAL_SECRET` 仍需编辑 `.env` 并重建容器。CLI `mymihomo update` 使用独立的容器本地凭据，网页改密后无需更新脚本。

`.env`、`conf/` 和 `.build/` 不提交 Git。备份同样包含订阅凭据，保持私有。
初次升级自动迁移原运行配置、订阅链接、定时表达式以及自定义覆写文件；迁移仅执行一次。
已有浏览器会话中的旧离线缓存会被注销，首次请刷新并登录。

## 网页端口显示

设置页端口为只读，表示容器内部监听值。网页修改监听端口不能自动改 Docker
主机映射，所以本部署会拒绝这种修改。如需把主机访问端口从 7897 改到 7898，
可只调整 Compose 左侧主机端口为 `127.0.0.1:7898:7897`；如需连容器端口
一起修改，则同步修改 `MIXED_PORT`、Compose 环境覆盖和映射右侧后重建。
代理端口仍为 7897，已增加指定局域网 IP 的映射，便于手机和其他局域网设备连接。
