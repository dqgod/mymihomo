# mymihomo

基于 Mihomo 和 zashboard 的服务端代理管理。开发分支为 `dev`。
前端源码随本仓库维护，当前基于 [zashboard v3.30.0](https://github.com/Zephyruso/zashboard/releases/tag/v3.30.0)。

## Web 使用

打开部署地址 `http://<服务器局域网IP>:9097`，输入 `WEB_CONFIG_SECRET` 登录。
未设置此项时使用 `EXTERNAL_SECRET`。通过设置页修改过密码后，以保存的新密码为准。登录后自动连接当前实例，不需要再填写 API 地址或内核密钥。
会话有效期 12 小时；退出登录或重启容器后需要重新登录。
设置页的“访问与安全”提供修改访问密码和退出登录。修改密码需验证当前密码并确认新密码；保存后所有会话及 WebSocket 失效，重新登录即可，代理服务不重启。密码以 bcrypt 摘要保存在 `conf/auth.json`，重启后仍生效，不修改内核密钥。

原导航页、Dashboard 和本项目的 YACD 已移除，统一使用 zashboard。

在侧边栏的 **订阅** 页面：

- 粘贴远程订阅链接导入，或通过“新建”导入本地 YAML 文件。
- 点击“使用”切换当前配置；切换使用已下载并保存的内容，不自动再次下载。
- “更新”下载远程订阅；更多菜单可指定直连或代理。当前订阅更新成功后自动应用，其他订阅只更新缓存。
- “编辑信息”设置名称、链接、五列 Cron、代理、User-Agent、超时、重试和 Base64 解码。
- 定时器读取持久化订阅信息，保存后立即生效。`30 12 * * *` 表示每天 12:30，按页面显示的时区执行（默认 `Asia/Hong_Kong`）；留空关闭。
- 远程订阅的原始文件、规则、节点和代理组只读，更多菜单可查看。只有本地配置允许直接编辑；修改远程订阅请使用覆写或脚本。仍支持排序、下载、分享二维码和删除。
- 流量、到期时间与主页来自订阅响应头；服务商未提供时不显示。
- 全局覆写与脚本对所有订阅生效。删除当前订阅前需要先切换配置。

**更新链路：** 原始订阅 → 全局 YAML 覆写 → 全局 JavaScript → 单订阅覆写 → 单订阅脚本 → 部署端口和内核访问设置 → Mihomo 校验 → 热加载。
下载、脚本或校验失败时，保留上次有效配置。远程原始内容在界面和 API 中均禁止修改；覆写和脚本会在每次更新时重新应用。

覆写示例：

```yaml
prepend-rules:
  - DOMAIN-SUFFIX,example.com,DIRECT
dns:
  enable: true
```

对象递归合并；普通数组替换；`prepend-` / `append-` 前缀在数组前后追加，`null` 删除键。规则通常用 `prepend-rules`，避免排在 `MATCH` 之后。

脚本示例（与 Clash Verge 的 `main` 入口兼容）：

```js
function main(config, profileName) {
  config.rules = ['DOMAIN-SUFFIX,example.com,DIRECT', ...(config.rules || [])];
  return config;
}
```

脚本必须同步返回配置对象，最多运行 2 秒，不提供文件、网络、Node.js 或进程接口；不支持依赖这些接口的桌面端脚本。二维码完全在浏览器内生成，不发送链接到外部服务。

## 查看与搜索

订阅页右上角“查看运行时配置”显示最近成功加载的完整 YAML，并叠加内核实时通用参数（例如运行模式），内核访问密钥隐藏。它与“查看原始文件”不同：前者包含覆写和脚本结果，后者是下载后保存的原文。

查看/编辑配置弹窗支持 `Ctrl+F`（Mac 为 `⌘F`）在当前文本内查找，显示匹配计数和行号；Enter/F3 跳转下一个，Shift+Enter/F3 上一个，Esc 先关闭搜索。搜索不会修改内容。

代理组提供定位当前节点、整组延迟测试、延迟/名称排序和隐藏不可用节点。排序与筛选按组保存在当前浏览器，默认跟随全局设置。定位会清除该组筛选并显示选中节点；不会切换代理节点。

## 数据和配置

| 文件/变量 | 用途 |
| --- | --- |
| `.env` | 部署密钥、端口和下载代理；位于 Compose 文件同级目录 |
| `conf/profiles.json` | 订阅链接、原始内容、定时、覆写和脚本（包含私密数据） |
| `conf/config.yaml` | 经过校验的当前运行配置 |
| `conf/web.env` | 旧版迁移来源；首次迁移后由 profiles.json 管理订阅 |
| `WEB_CONFIG_SECRET` | 首次/未设置网页密码时使用的初始密码 |
| `conf/auth.json` | 网页保存的密码摘要，优先于环境变量 |
| `conf/.auth-cli-token` | 自动生成的容器本地 CLI 认证凭据 |
| `EXTERNAL_SECRET` | 内核 API 密钥，仅网关/内核使用 |
| `WEB_CONFIG_PORT` | 网关端口，默认 9097 |
| `EXTERNAL_PORT` | 容器内部内核 API 端口，默认 19097；不向主机映射 |
| `MIXED_PORT` | HTTP/SOCKS 混合代理端口，默认 7897 |
| `HTTPS_PROXY` / `HTTP_PROXY` | 订阅选择“代理更新”且未指定代理时使用的下载代理 |
| `CONF_URL` / `CRON_EXPRESSION` | 首次初始化订阅使用；迁移后请在网页中修改 |
| `TZ` | 首次初始化的时区，默认 Asia/Hong_Kong |

订阅里的 API 绑定、密钥、代理端口不能覆盖部署设置。TUN 默认关闭。端口在设置页只读：网页不能修改 Docker 映射，需同步调整 `.env` 与 `compose.local.yaml` 并重建容器。API 也会拒绝修改这些端口。原生 zashboard 设置页仍可以调整运行模式等状态；需要在后续订阅更新后保留的 YAML 设置请写入全局覆写。网页访问密码修改即时生效；部署内核密钥修改需重建容器。

## 构建

```sh
# Docker 构建包含 Go 测试、前端类型检查和源码构建
# CORE_IMAGE 可指定所需的 Mihomo 镜像
docker build -t mymihomo:dev .
```

本机旧 CPU 使用兼容内核和缓存构建流程，见 [LOCAL_DEPLOYMENT.md](LOCAL_DEPLOYMENT.md)。
前端独立开发：Node.js 24、pnpm（packageManager 锁定版本）、`scripts/build-ui.sh`。
Go 开发：Go 1.23+，`go test ./...`。
升级前端时按 [UPSTREAM.md](web/zashboard/UPSTREAM.md) 合并新版本；不要使用面板的在线覆盖升级，避免丢失本仓库的集成功能。

## CLI 与 API

容器内 `mymihomo update` 更新当前订阅，与网页和定时器共用管理服务及串行锁。
`mymihomo prepare` 在启动时从持久化订阅生成运行配置；已有数据时不会重新下载订阅。
`mymihomo download` 是旧版独立下载工具，不参与网页更新流程。

管理 API 为同源 `/api/profiles`；浏览器使用 HttpOnly、SameSite 会话 Cookie，脚本可用 `Authorization: Bearer <当前访问密码>`。
写请求校验 Origin，内核 REST/WebSocket 均经登录网关代理。旧 `/api/config` 已退出使用，18087 不再映射。
