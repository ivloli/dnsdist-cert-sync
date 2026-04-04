# dnsdist-cert-sync 技术方案

## 1. 背景与目标

当前 dnsdist 的 TLS 证书通过 SSH 手工更新，存在以下问题：

- 证书更新依赖人工操作，容易遗漏多机同步。
- 证书切换窗口不可控，回滚成本高。
- 缺少统一审计与变更来源追踪。

本方案新增独立服务 `dnsdist-cert-sync`，通过 Nacos 配置中心下发证书，并在本机原子落盘后触发 dnsdist 热重载，实现无停机更新。

推荐与中心发布服务 `cert-publisher` 配合使用：由 `cert-publisher` 统一续期并发布到 Nacos，`dnsdist-cert-sync` 仅负责节点侧消费与热加载。

## 2. 系统边界

- **证书来源**：Nacos（`namespace/group/data_id` 可配置）
- **证书消费方**：dnsdist（`addTLSLocal()` 使用的 crt/key 文件）
- **同步服务**：`dnsdist-cert-sync`（本次新增）

## 3. 核心流程

`dnsdist-cert-sync` 启动后执行：

1. 加载 YAML 配置（支持环境变量展开）。
2. 初始化 Nacos ConfigClient。
3. 先做一次启动同步（`GetConfig`）。
4. 注册 `ListenConfig` 监听增量变更。
5. 兜底轮询（`poll_interval`），避免监听回调丢失。

每次拿到新配置内容时：

1. 计算内容哈希，未变化则跳过。
2. 解析证书 JSON（支持常见字段别名，如 `cert/crt/certificate`、`key/private_key`）。
3. 校验证书与私钥匹配、证书有效期合法。
4. 原子写入目标文件（`*.tmp -> rename`）。
5. 调用 dnsdist 控制命令热重载证书。
6. 记录新旧证书指纹（便于审计）。

证书 JSON 字段兼容（重点）：

- 优先读取：`certificate_fullchain_pem` / `private_key_pem`
- 同时兼容：`certificate_pem`、`cert`、`key`、`ca`

## 4. 配置说明

参考文件：`dnsdist-cert-sync/config.prod.yaml`

- `nacos.*`：Nacos 地址、命名空间、分组、DataID、账号密码。
- `cert.*`：证书与私钥落盘路径（建议直接复用 dnsdist 当前路径）。
- `dnsdist.*`：控制面重载参数。
  - 推荐模式：`binary_path + control_addr + control_key + reload_lua_command`
  - 兼容模式：直接配置 `reload_command`（完整 shell 命令）
- `sync.poll_interval`：监听之外的兜底拉取周期。

### 4.1 配置项详解（新服务）

#### nacos

- `nacos.addr`：Nacos 地址，格式 `host:port`。
- `nacos.namespace`：命名空间（空则默认 public）。
- `nacos.group`：证书配置所在 Group。
- `nacos.data_id`：证书配置所在 Data ID。
- `nacos.username` / `nacos.password`：Nacos 登录凭据（建议走 env 注入）。

#### cert

- `cert.cert_file`：证书落盘路径（建议直接复用 dnsdist 的 `addTLSLocal` 路径）。
- `cert.key_file`：私钥落盘路径。
- `cert.chain_file`：可选，单独写中间证书链。
- `cert.raw_dump_file`：可选，将 Nacos 原始 JSON 落盘，便于排障。

#### dnsdist

- `dnsdist.binary_path`：dnsdist 二进制路径。
- `dnsdist.control_addr`：control socket 地址（如 `127.0.0.1:15199`）。
- `dnsdist.control_key`：control key（建议 env 注入）。
- `dnsdist.reload_lua_command`：热重载 Lua 命令，默认 `reloadAllCertificates()`。
- `dnsdist.reload_command`：可选，若填写则直接执行该 shell 命令，覆盖上述 control 参数。

#### sync

- `sync.poll_interval`：轮询周期（建议 30s~60s）；用于监听丢事件时兜底。

### 4.2 配置示例（可直接参考）

示例配置（可直接参考）：

```yaml
nacos:
  addr: "172.31.40.191:8848"
  namespace: "dnps-dev"
  group: "certs"
  data_id: "66"
  username: "${NACOS_USERNAME}"
  password: "${NACOS_PASSWORD}"

cert:
  cert_file: "/etc/dnsdist/ufaei.com.crt"
  key_file: "/etc/dnsdist/ufaei.com.key"
  raw_dump_file: "/etc/dnsdist/nacos_cert.json"

dnsdist:
  binary_path: "/usr/bin/dnsdist"
  control_addr: "127.0.0.1:15199"
  control_key: "${DNSDIST_CONTROL_KEY}"
  reload_lua_command: "reloadAllCertificates()"

sync:
  poll_interval: "30s"
```

配套环境变量文件 `/etc/dnsdist-cert-sync/env`：

```bash
NACOS_USERNAME=nacos
NACOS_PASSWORD=nacos@2025
DNSDIST_CONTROL_KEY=ETwCI8dkSoM4YAl1qmrzI3PXGJ0emm9FsG0nrbodJE8=
```

### 4.3 运行时行为说明（上述配置）

启动后实际行为：

1. 连 Nacos 读取 `group=certs, data_id=66`。
2. 解析证书 JSON，写入：
   - `/etc/dnsdist/ufaei.com.crt`
   - `/etc/dnsdist/ufaei.com.key`
3. 执行相当于：
   - `dnsdist -c 127.0.0.1:15199 -k "$DNSDIST_CONTROL_KEY" -e 'reloadAllCertificates()'`
4. 持续监听 Nacos 变更，且每 `30s` 兜底轮询一次（`sync.poll_interval`）。

### 4.4 一套可执行命令（部署到机器）

```bash
# 1) 构建并安装
make build-dnsdist-cert-sync
sudo make install-dnsdist-cert-sync

# 2) 编辑配置（按实际 Nacos 地址、data_id 调整）
sudo vi /etc/dnsdist-cert-sync/config.yaml

# 3) 写入敏感变量
sudo vi /etc/dnsdist-cert-sync/env
# 示例：
# NACOS_USERNAME=nacos
# NACOS_PASSWORD=nacos@2025
# DNSDIST_CONTROL_KEY=ETwCI8dkSoM4YAl1qmrzI3PXGJ0emm9FsG0nrbodJE8=

# 4) 启动服务
sudo systemctl daemon-reload
sudo systemctl enable dnsdist-cert-sync
sudo systemctl restart dnsdist-cert-sync

# 5) 查看状态与日志
sudo systemctl status dnsdist-cert-sync
sudo journalctl -u dnsdist-cert-sync -f
```

### 4.5 验证命令

```bash
# 观察证书文件是否更新
sudo ls -l /etc/dnsdist/ufaei.com.crt /etc/dnsdist/ufaei.com.key

# 看同步服务是否执行了热重载
sudo journalctl -u dnsdist-cert-sync -n 200 --no-pager | grep -E 'applied from|reload output|fingerprint'
```

## 5. 为什么证书路径建议复用 dnsdist

建议直接写入 dnsdist 现有证书路径（如 `/etc/dnsdist/ufaei.com.crt`、`/etc/dnsdist/ufaei.com.key`）：

- 不需要改现有 `addTLSLocal()`。
- 重载语义简单，避免路径切换导致误配。
- 运维视角更一致（故障排查路径固定）。

仅在“蓝绿证书切换”场景才建议独立路径并配套切换策略。

## 6. 安全与稳定性设计

- 证书写盘采用原子替换，避免半写入文件被 dnsdist 读取。
- 私钥文件权限使用 `0600`。
- 先校验再覆盖，减少坏证书上线风险。
- 监听 + 轮询双通道，降低丢事件风险。
- 变更日志包含证书指纹，不直接打印证书内容。

## 7. 部署方式

### 7.1 构建与安装

```bash
make build-dnsdist-cert-sync
sudo make install-dnsdist-cert-sync
```

### 7.2 配置敏感信息

编辑 `/etc/dnsdist-cert-sync/env`：

```bash
NACOS_USERNAME=...
NACOS_PASSWORD=...
DNSDIST_CONTROL_KEY=...
```

### 7.3 启动

```bash
sudo systemctl daemon-reload
sudo systemctl enable dnsdist-cert-sync
sudo systemctl start dnsdist-cert-sync
sudo systemctl status dnsdist-cert-sync
```

## 8. 观测与验证

- 服务日志：`journalctl -u dnsdist-cert-sync -f`
- 关键日志关键词：
  - `applied from startup/listen/poll`
  - `reload output`
  - `cert fingerprint old -> new`

联调时可在 Nacos 修改证书，观察：

1. `dnsdist-cert-sync` 出现 apply + reload 日志
2. dnsdist TLS 握手使用新证书

## 9. 失败处理策略

- 解析失败：跳过本次更新，保留旧证书。
- 证书校验失败：拒绝落盘，保留旧证书。
- dnsdist 重载失败：返回错误日志，等待下一次配置变更或轮询重试。

## 10. 与 CoreDNS XDB 自动更新联动（后续）

配套改造已经落地：

- `subnet-manager` 在同步成功后，会将 `ip2region_version` 发布到 Nacos。
- `coredns ecs_normalizer` 监听该版本 Key：
  - 检测版本变化 -> 下载新 XDB -> 热重载内存 searcher -> 写本地版本文件。

这样证书与 XDB 都能通过 Nacos 实现集中变更与热更新，减少手工 SSH 运维。
