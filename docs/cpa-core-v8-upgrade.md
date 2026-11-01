# CPA-Core-LTS v1-lts-0.1.2 升级说明

本次适配针对 Core `v1-lts-0.1.2`（`460c177cc90744faa374185bc3efef5a1f4c0b67`），配套 Panel 为 `v1-lts-0.1.1`。
源码与配置提交不代表生产升级完成。不得仅切换镜像后继续将旧配置当作受支持的 V8 配置使用。

## 已核对的运行情况

2026-11-01 通过 SCM 只读作业读取配置，在主机端脱敏后回传：

| 租户 | 主机 | 实际镜像标签 | 配置 | 容器监听端口 |
| --- | --- | --- | --- | --- |
| hanada | as-jp-13-ntt-512 | v1-lts-0.0.48 | `/usr/local/cpa/cpa_hanada/data/config.yml` | 18316 |
| huachienergy | as-jp-13-ntt-515 | v1-tls-0.0.21 | `/usr/local/cpa/cpa_huachienergy/data/config.yml` | 18319 |

两套都是旧布局，插件均关闭。huachienergy 的实际镜像比配置仓库的 `v1-lts-0.0.48` 更旧，回退必须以主机真实镜像 ID/标签为准。
未回传真实管理密钥、客户端密钥或上游密钥；未更改运行配置、认证文件或容器。

## 破坏性变更

| 旧位置 | V8 位置 / 行为 |
| --- | --- |
| `host`、`port`、`tls`、`discovery` | `server.*` |
| `remote-management` | `management` |
| 根 `api-keys` 客户端密钥数组 | `access.api-keys`；根 `api-keys` 现在是提供商分组对象 |
| `codex-api-key` 等扁平数组 | `api-keys.codex` 等分组：公共设置 + `keys` 数组 |
| `auth-dir`、OAuth 别名/排除规则 | `oauth.auth-dir`、`oauth.model-alias`、`oauth.excluded-models` |
| `codex.identity-confuse`、异常重试等 | `upstream.codex.*`；OAuth 专有字段仍在 `oauth.providers.codex.*` |
| `codex.optimize-multi-agent-v2` | `client.codex.optimize-multi-agent-v2` |
| `payload`、代理、流式保活 | `requests.*` |
| 日志、用量统计、队列保留 | `observability.logs.*`、`observability.usage.*` |
| `PUT /v0/management/config.yaml` | 不允许替换 V8 配置，返回 409 |
| V8 配置 PUT/PATCH/DELETE | 必须提交读取快照时的强 `ETag` 为 `If-Match`，冲突返回 412，缺失返回 428 |

`false`、`0`、空列表/对象与缺失不同；每密钥的 `null` 表示继承组设置。不能在升级中用真假判断丢弃这些值。
`/v1/*` 模型调用路径不变。完整 usage、usage queue、旧客户端密钥管理和凭证操作的 V0 接口仍被 Core 保留，不需要重建用户密钥或重算历史账单。

## CPA-Helper 0.3.23.123

- 系统设置优先使用 `/v8/management/config.yaml`，按 V8 路径读取和保存可视化字段。
- 客户端密钥不会误写到上游提供商分组；源文件编辑继续保留未编辑字段和注释。
- 原始 ETag 随编辑快照传递；冲突保留草稿，要求重新加载核对，不自动使用新 ETag 重放旧草稿。
- 上游管理通过 V8 原生分组接口读写。现有逐凭证界面保留组名、兄弟密钥和每密钥继承/覆盖；只修改一个密钥的服务地址时，将该密钥拆入新组，不改动其他密钥的地址。
- 仅 V8 路由确实返回 404 时使用旧版接口，允许先升级 Helper、后升级 Core。401/403、服务器错误和缺少配置版本不会触发降级写入。
- `enable-gemini-cli-endpoint` 已不属于新版本支持的配置，不再出现在 V8 可视化表单。

## 生成候选配置（可由 SCM 下发 Python 脚本）

`scripts/prepare_cpa_v8.py` 是本项目为这次部署准备的辅助工具，不是 Core 官方迁移工具。要求 `python3` 和 PyYAML。
它默认只读；只有提供 `--output` 才创建新的 `0600` 候选文件，拒绝覆盖源文件或已有候选。日志仅包含字段移动清单、校验和及条目数量，不打印密钥。

```sh
# 512：只读检查
python3 prepare_cpa_v8.py --source /usr/local/cpa/cpa_hanada/data/config.yml

# 515：只读检查
python3 prepare_cpa_v8.py --source /usr/local/cpa/cpa_huachienergy/data/config.yml

# 确认后，分别生成候选；不要覆盖运行中的 config.yml
python3 prepare_cpa_v8.py --source /usr/local/cpa/cpa_hanada/data/config.yml --output /usr/local/cpa/cpa_hanada/data/config.v8.candidate.yml
python3 prepare_cpa_v8.py --source /usr/local/cpa/cpa_huachienergy/data/config.yml --output /usr/local/cpa/cpa_huachienergy/data/config.v8.candidate.yml
```

该工具不保留 YAML 注释/排版，但保留配置值与密钥；不会将未知字段静默删除。未知根字段、混合布局、重复键、YAML merge key 等情况会拒绝自动生成，需单独审查。
生成候选不等于完整 Core 校验或真实上游验收。候选必须通过目标版本验证，并逐项核对客户端密钥数量、提供商及模型映射、优先级、禁用状态、FAST 规则、OAuth 排除规则、统计开关和队列保留期。
准备后若运行配置又被修改，须重新生成候选；以工具输出的源文件 SHA-256 确认对应的配置快照。

## 切换顺序与恢复

1. 先发布 Helper `0.3.23.123`，确认仍能连接现有 Core，并能读取旧配置、用量和凭证。
2. 分别备份实际 Compose、运行镜像 ID/标签、`data/config.yml`、认证文件，以及当前可导出的 canonical v3 usage。备份保留为私有文件，不进入普通日志或 Git。
3. 生成并审查各自 V8 候选，使用新版本与隔离的配置/认证目录进行验证；不得把生产可写认证目录挂给测试实例，也不要启动第二个生产用量采集器。
4. 正式切换前暂停该实例的管理写入和流量，记录最新源配置校验和，重新核对候选。停旧容器后才安装候选，避免旧 Core watcher 提前读取 V8 配置。
5. 通过 SCM 发布对应 CPA 租户的 `v1-lts-0.1.2` 镜像。配置仓库的 `pre_recreate` 只读检查会拒绝旧布局/错误端口/错误认证目录，不自动迁移或改写配置。
6. 确认配套 Panel `v1-lts-0.1.1` 已实际加载；旧缓存面板不能安全编辑 V8。验证 `/healthz`、管理鉴权、Helper 设置读取/保存、客户端密钥、凭证、上游及真实受控请求，用量队列应持续入库且无重复计费。
7. 一套稳定后再切另一套。失败时先停新容器，恢复实际旧镜像、原配置和配套 Panel/插件，再恢复流量。认证刷新已发生的远端状态不能靠复制旧本地 auth 文件保证逆转。

未获得正式切换确认前，只准备代码、配置版本与候选，不对生产执行步骤 4–7。

## 上游依据

- https://github.com/BlueSkyXN/CPA-Core-LTS/releases/tag/v1-lts-0.1.2
- https://github.com/BlueSkyXN/CPA-Core-LTS/blob/v1-lts-0.1.2/docs/lts/v8-development-compatibility.md
- https://github.com/BlueSkyXN/CPA-Core-LTS/blob/v1-lts-0.1.2/docs/management-api-v8.md
- https://github.com/BlueSkyXN/CPA-Core-LTS/blob/v1-lts-0.1.2/config.example.yaml
