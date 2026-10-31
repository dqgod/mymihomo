# mymihomo 开发约定

开发分支 `dev`，维护仓库 `dqgod/mymihomo`。这是服务端 Mihomo + 带登录网关的 zashboard，前端基于 v3.30.0。

## 边界

- 不得停止、重启或修改原 `clash` 容器；新实例为 `mymihomo-dev`，代理 7897。
- LAN 仅绑定指定 `MYMIHOMO_LAN_IP` 和回环地址，不扩大暴露到所有网卡。
- 9097 是登录网关，内核 19097 仅容器回环。不得绕过登录发布静态 UI/内核 API。
- `.env`、`conf/`、`.build/` 含密钥、订阅或构建资源，不提交。不要输出订阅链接和密钥。
- 不通过上游在线升级覆盖定制前端；按 `web/zashboard/UPSTREAM.md` 合并源码。

## 结构

- `cmd/mymihomo/main.go`：CLI 入口；`envfile.go`：旧环境文件迁移读取。
- `profiles.go`：原子存储、下载、事务应用、调度和部署边界。
- `profiles_api.go`：订阅 CRUD、切换、更新、内容/扩展编辑、排序和导出。
- `extensions.go`：YAML 合并及限时 JavaScript 变换。
- `serve.go`：登录会话、受保护静态资源、管理 API、内核 REST/WebSocket 网关。
- `auth.go`：持久化密码摘要、会话/升级连接撤销和容器本地 CLI 认证。
- `AccessSettings.vue`：设置页密码修改和退出登录入口。
- `update.go`：内核校验、热加载、启动准备和 CLI 更新桥接。
- `download.go`：保留的旧版独立下载工具，不用于新服务更新路径。
- `*_test.go`：认证、脚本、事务失败、时区、定时更新与代理测试。
- 远程订阅原文/分区只读，后端必须拒绝直接写入；仅本地配置可直接编辑。部署端口也必须在网关拦截修改。
- `SearchableEditor.vue`：弹窗内查找；`ProxyGroupActions.vue`：按组定位、测速、排序和筛选。
- `web/zashboard/`：带 MIT 许可的上游源码及本项目集成。
- `run.sh`：准备配置并监督内核与网关；任一进程退出时容器退出重启。
- `Dockerfile`：完整源码构建；`Dockerfile.local`：本机 compatible 内核及预构建前端。
- `compose.local.yaml`：独立实例，私有配置和 runtime 卷。
- `scripts/build-ui.sh`：锁定依赖安装、类型检查和前端构建。
- `scripts/tests/integration.py`：隔离容器的真实内核与浏览器集成验证。
- `scripts/prepare-ui.py`：兼容旧构建入口，转调源码构建，不下载旧面板。

## 验证

Go 1.23+：`go test ./...`；前端 Node 24：`scripts/build-ui.sh`。
修改鉴权、更新链路或调度时，验证未登录拒绝、登录后页面/API/WebSocket、失败保留配置、时区与重启持久化。
部署前在独立实例验证真实 Mihomo 校验和热加载；部署后检查新实例健康和原 Clash 容器 ID/启动时间未变。
新增文件或更改职责后同步更新本文与 README/部署文档。
