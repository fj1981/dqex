# FAQ / 常见问题

Common questions for new dqex users. / 面向 dqex 新用户的常见问题。

> English / 简体中文

## English

### What if port 8181 is already in use?

Before starting, the Web service checks whether the default port (`8181`) is in use. On a conflict you can either let dqex terminate the occupying process and rebind, or open the already-running service. Alternatively, start on another port with `--port <port>`:

```sh
dqex --port 8282
```

To stop a running dqex process, use `dqex stop`.

### How to access the Web UI from another machine?

By default the Web service binds only to the local loopback address (`127.0.0.1`) and requires no authentication. To reach it from another machine on the same network:

1. Start with `--host 0.0.0.0` so it listens on all interfaces:

   ```sh
   dqex --host 0.0.0.0
   ```

2. Open `http://<machine-ip>:8181` from the other machine.

3. External sources are protected by **token authentication**: loopback requests are auth-free, while other hosts must present a valid token.

> ⚠️ Never run with `--no-auth` on an exposed network; it disables all authentication.

### How to save a connection in the CLI?

Use `dqex conn add` to store a frequently used connection so you do not re-enter credentials every time:

```sh
dqex conn add --name prod --type mysql --host 10.0.0.5 --port 3306 --user app --password <pwd>
```

Saved connections can be listed and referenced later (see `dqex conn --help`).

### How is the AI entry hidden when not configured?

The AI assistant is a fully optional, zero-intrusion add-on gated by a single check (`AIEnabled`): the `ai` section in `config.yaml` must be present and both `base_url` and `api_key` must be non-empty (`api_key` also falls back to the `DBX_AI_API_KEY` env var).

When the check fails:

- The Web AI panel/toolbar button is not rendered at all — no trace of the feature.
- The CLI `\h` help does not list AI meta-commands (`\ai ...`).
- No LLM client is initialized and no requests are made.

Once you save a valid configuration (the Settings page applies changes hot, no restart needed), the entry appears immediately.

### Where are export files stored?

Export files are written to a configurable output directory. To check the current location, run:

```sh
dqex config
```

The export directory is typically `exports` or `.dqex-exports` under the data directory.

---

## 简体中文

### 端口 8181 被占用怎么办？

Web 服务启动前会自动检测默认端口（`8181`）是否被占用。发现冲突时，你可以让 dqex 终止占用进程后重新绑定，或直接打开已存在的服务；也可以用 `--port <端口>` 改用其他端口启动：

```sh
dqex --port 8282
```

如需终止正在运行的 dqex 进程，使用 `dqex stop`。

### 如何跨机器访问 Web 界面？

默认 Web 服务只绑定本机回环地址（`127.0.0.1`），本机访问免认证。要跨机器访问：

1. 使用 `--host 0.0.0.0` 监听所有网卡：

   ```sh
   dqex --host 0.0.0.0
   ```

2. 在另一台机器上打开 `http://<机器 IP>:8181`。

3. 外部来源由**令牌认证**保护：本机回环请求免认证，其他来源必须携带有效令牌。

> ⚠️ 切勿在对外暴露的网络中使用 `--no-auth`，它会完全禁用认证。

### 命令行如何保存常用连接？

使用 `dqex conn add` 保存常用连接，避免每次重复输入连接信息：

```sh
dqex conn add --name prod --type mysql --host 10.0.0.5 --port 3306 --user app --password <密码>
```

已保存的连接可后续列出并引用（详见 `dqex conn --help`）。

### AI 未配置时如何隐藏 AI 入口？

AI 助手是完全可选、零侵入的增量功能，由单一判定（`AIEnabled`）门控：`config.yaml` 中的 `ai` 段存在，且 `base_url` 与 `api_key` 均非空（`api_key` 支持环境变量 `DBX_AI_API_KEY` 兜底）。

判定不通过时：

- Web 端 AI 面板/工具栏按钮完全不渲染，不留任何 AI 痕迹；
- CLI `\h` 帮助中不列出 AI 元命令（`\ai ...`）；
- 不初始化 LLM 客户端，不发起任何请求。

在设置页保存有效配置后（保存即热生效，无需重启），入口会立即出现。

### 导出文件在哪里？

导出文件写入可配置的输出目录。运行以下命令查看当前配置（含导出目录）：

```sh
dqex config
```

导出目录通常位于数据目录下的 `exports` 或 `.dqex-exports`。
