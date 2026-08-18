# Deploy Agent

`bedrock-agent` 是运行在**部署目标机**上的独立 HTTP 服务。Bedrock Server 通过它上传制品并在目标机执行 post-deploy 脚本，适用于不便开放 SSH、或希望仅用 HTTP 完成分发的场景。

> **注意区分**：本页的 Deploy Agent 与「AI → 智能体（Agent）」无关。后者是平台内的 AI 运行单元；Deploy Agent 是 CI/CD 分发用的远端二进制。

---

## 1. 适用场景

| 场景             | 说明                                             |
| ---------------- | ------------------------------------------------ |
| 目标机不开放 SSH | 仅放行 Bedrock Server → Agent 的 HTTP(S)         |
| Windows 部署目标 | 发布包含 `bedrock-agent-windows-amd64.exe`       |
| 内网单向访问     | Server 主动连目标机 Agent，无需目标机回连 Server |
| 与 SSH 分发并存  | 同一平台可同时登记 SSH 服务器与 Agent 服务器     |

不适合：需要 `rsync --delete` 镜像同步时（`mirror` 仅对 rsync 生效；Agent 分发为覆盖式解压，不删除远端多余文件）。

---

## 2. 架构

```text
Bedrock Server（构建机）
    │  HTTP(S)  Bearer Token
    │  POST /upload   — 上传制品压缩包并解压到 remote_path
    │  POST /exec     — 执行 post-deploy 脚本
    ▼
目标机 bedrock-agent（默认 :9091）
    └── 本地文件系统（remote_path）
```

- Agent **不嵌入** Server，须单独部署在每台需要 HTTP 分发的目标机上。
- 网络方向：**Server → Agent**（确保 Server 能访问 `agent_url`；Agent 无需访问 Server）。
- 发布物须与 Server **同版本**（如 `bedrock-linux-amd64` 配 `bedrock-agent-linux-amd64`）。

---

## 3. 目标机安装

```bash
# 1. 从发布包取得对应平台二进制（示例：Linux amd64）
#    bedrock-agent-linux-amd64 + .sha256

sha256sum -c bedrock-agent-linux-amd64.sha256   # 校验

# 2. 安装到固定目录
sudo install -m 0755 bedrock-agent-linux-amd64 /opt/bedrock/bedrock-agent

# 3. 编写配置文件（与二进制同目录，或通过 -config 指定）
sudo tee /opt/bedrock/bedrock-agent.yaml <<'EOF'
addr: ":9091"
token: "请替换为足够长的随机字符串"
# 生产建议启用 TLS（见 §8）
# tls_cert: "/etc/bedrock/agent.crt"
# tls_key:  "/etc/bedrock/agent.key"
EOF
sudo chmod 0600 /opt/bedrock/bedrock-agent.yaml

# 4. 前台试跑（确认无报错后 Ctrl+C，再配 systemd）
/opt/bedrock/bedrock-agent -config /opt/bedrock/bedrock-agent.yaml
```

Windows：将 `bedrock-agent-windows-amd64.exe` 与 `bedrock-agent.yaml` 放在同一目录，在 PowerShell 中执行即可；默认读取同目录下的 `bedrock-agent.yaml`。

---

## 4. 配置文件（bedrock-agent.yaml）

| 字段       | 必填   | 默认值  | 说明                                                                            |
| ---------- | ------ | ------- | ------------------------------------------------------------------------------- |
| `addr`     | 否     | `:9091` | 监听地址，如 `:9091` 或 `127.0.0.1:9091`                                        |
| `token`    | **是** | —       | Bearer 认证令牌；Server 调用时须在 `Authorization: Bearer <token>` 中携带相同值 |
| `tls_cert` | 否     | —       | TLS 证书路径；与 `tls_key` 同时配置时启用 HTTPS                                 |
| `tls_key`  | 否     | —       | TLS 私钥路径                                                                    |

配置优先级（高 → 低）：命令行参数 → 环境变量 → YAML 文件 → 默认值。

| 命令行      | 环境变量                                                        |
| ----------- | --------------------------------------------------------------- |
| `-addr`     | `BEDROCK_AGENT_ADDR`                                            |
| `-token`    | `BEDROCK_AGENT_TOKEN`                                           |
| `-tls-cert` | `BEDROCK_AGENT_TLS_CERT`                                        |
| `-tls-key`  | `BEDROCK_AGENT_TLS_KEY`                                         |
| `-config`   | —（指定 YAML 路径；默认 `<可执行文件目录>/bedrock-agent.yaml`） |

`token` 未配置时进程会拒绝启动。

---

## 5. systemd 示例（Linux）

```ini
# /etc/systemd/system/bedrock-agent.service
[Unit]
Description=Bedrock Deploy Agent
After=network.target

[Service]
Type=simple
ExecStart=/opt/bedrock/bedrock-agent -config /opt/bedrock/bedrock-agent.yaml
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable --now bedrock-agent
sudo systemctl status bedrock-agent
```

---

## 6. 平台侧配置

按顺序完成以下三步，再在「资源管理 → 服务器」列表中点「测试连接」验证。

### 6.1 创建 Agent 凭证

路径：**资源管理 → 凭证 → 新建**

| 字段 | 填写                                                           |
| ---- | -------------------------------------------------------------- |
| 类型 | `token`                                                        |
| 密钥 | 与目标机 `bedrock-agent.yaml` 中 **`token` 完全一致** 的字符串 |

绑定到服务器记录需要 `resource_credentials:use` 权限。

> Agent 进程强制要求 token；平台侧也应绑定凭证，否则连接测试与分发无法携带 `Authorization`，会返回 `401 unauthorized`。

### 6.2 登记服务器

路径：**资源管理 → 服务器 → 新建**

| 字段                 | Deploy Agent 模式                                                              |
| -------------------- | ------------------------------------------------------------------------------ |
| 认证方式             | `Deploy Agent`                                                                 |
| Agent URL            | Agent 的 HTTP 根地址，如 `http://10.0.0.5:9091` 或 `https://agent.example.com` |
| Agent 凭证           | 上一步创建的 `token` 凭证                                                      |
| OS                   | `linux` 或 `windows`（影响远端路径分隔符与脚本解释器）                         |
| 主机 / 端口 / 用户名 | **无需填写**（`auth_type=agent` 时不走 SSH）                                   |

`agent_url` 只填**基础地址**，不要带 `/healthz`、`/upload` 等路径后缀。

### 6.3 构建任务绑定部署目标

路径：**CI/CD → 构建任务 → 部署目标**

| 字段       | 说明                                                                                     |
| ---------- | ---------------------------------------------------------------------------------------- |
| 服务器     | 选择上一步登记的 Agent 服务器                                                            |
| 分发方式   | **`agent`**                                                                              |
| 远端路径   | 目标机绝对路径，如 `/var/www/myapp`（Windows 如 `C:\apps\myapp`）                        |
| 部署后脚本 | 可选；通过 Agent `/exec` 在 `remote_path` 下执行（Linux 用 `sh`，Windows 用 PowerShell） |

---

## 7. 分发行为

1. Server 将构建制品打成压缩包（默认 tar.gz，亦支持 zip），`POST` 到 Agent `/upload`。
2. 请求头 `X-Target-Path` 为部署目标的 `remote_path`；Agent 创建目录并解压覆盖同名文件。
3. Agent **不会**删除 `remote_path` 中多余的旧文件（与 rsync merge 类似，而非 mirror）。
4. 若配置了 post-deploy 脚本，Server 再 `POST` `/exec`，body 为 `{"script":"...","work_dir":"<remote_path>"}`。

---

## 8. 探活与手动验证

在 **Bedrock Server 所在机器**（或任何能访问 Agent 的网络位置）执行：

```bash
# 将 TOKEN、AGENT_URL 替换为实际值
curl -fsS -H "Authorization: Bearer ${TOKEN}" "${AGENT_URL}/healthz"
# 期望：ok (linux <version>) 或 ok (windows <version>)
```

平台「测试连接」同样请求 `GET {agent_url}/healthz`（带 Bearer）。不要用 `/health`——该路径不存在，会返回 `404 page not found`。

---

## 9. 安全说明

1. **Token 即凭据**：等同于共享密钥，勿写入版本库；定期轮换时需同时改 Agent 配置与平台凭证。
2. **传输层**：Agent 默认明文 HTTP；公网或不可信网络请配置 `tls_cert` / `tls_key`，并将 `agent_url` 改为 `https://...`。
3. **监听范围**：若仅本机反代，可设 `addr: "127.0.0.1:9091"`，由 Nginx/Caddy 对外提供 TLS。
4. **执行权限**：Agent 以运行它的 OS 用户身份解压文件并执行脚本；与 SSH 分发一样，**不是** OS 沙箱。

---

## 10. 升级与回退

与 Server 同步操作：

1. 停止目标机 `bedrock-agent` 与 Server。
2. 替换为**同版本**新二进制（校验 SHA256）。
3. 若仅小版本升级且配置未变，保留 `bedrock-agent.yaml` 即可。
4. 启动 Agent → 平台「测试连接」→ 再启动 Server。

回退时同样保持 Server 与所有目标机 Agent 版本一致。

---

## 11. 常见故障

| 现象                                  | 可能原因                             | 处理                                                           |
| ------------------------------------- | ------------------------------------ | -------------------------------------------------------------- |
| `agent 返回 404`                      | URL 路径错误；或连到了别的 HTTP 服务 | 确认 `agent_url` 无多余路径；手动 `curl .../healthz`           |
| `401 unauthorized`                    | Token 不一致或未绑定凭证             | 核对 YAML `token` 与平台 `token` 凭证；服务器须绑定 Agent 凭证 |
| `agent 连接失败` / 超时               | 网络、防火墙、Agent 未启动           | 从 Server 主机 `curl` Agent URL；检查安全组/iptables           |
| `agent token is required`（构建日志） | 部署目标服务器未绑定凭证             | 编辑服务器，选择 Agent 凭证                                    |
| `agent_url 不能为空` / 无效           | 服务器记录配置不完整                 | 补全 `http://` 或 `https://` 前缀与主机端口                    |
| 解压成功但脚本失败                    | post-deploy 脚本或 `work_dir` 错误   | 查看构建日志中 Agent 返回；Windows 目标须用 PowerShell 语法    |
| 文件权限不对                          | Agent 运行用户无写权限               | 调整 `remote_path` 目录属主，或以合适用户运行 Agent            |
