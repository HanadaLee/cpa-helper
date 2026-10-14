# SCM 一键演练与正式迁移

使用单文件 `scripts/migrate-postgres.py`（Python 3.8+ 标准库）。无需在目标机安装 PostgreSQL 客户端、Python 依赖或 SQLite CLI。

## 已准备的两套配置

| 实例 | 安装目录 | 数据库 / 专用用户 | 本机应用端口 |
| --- | --- | --- | --- |
| hanada | `/usr/local/cpa-helper/cpa-helper_hanada` | `cpa_helper_hanada` | 18317 |
| huachienergy | `/usr/local/cpa-helper/cpa-helper_huachienergy` | `cpa_helper_huachienergy` | 18318 |

两套实例均位于 conf 当前声明的 `as-jp-13-oracle-50`，通过 SCM 分别发布。**没有代为执行生产迁移**。

- 两套独立的 64 位随机十六进制密码已写入私有 conf 仓库各租户的 `base/env/postgres-migration.env`，按 0600 权限下发到安装目录的 `postgres-migration.env`；不要重新生成或把内容复制到任务日志。
- 管理连接：`docker exec --user postgres postgresql psql -h /var/run/postgresql -p 63851 -U postgres_admin`，无需获取数据库管理员密码。
- 应用连接：`host.docker.internal:63851`，`CPA_HELPER_DB_MAX_CONNS=64`。专用用户没有超级用户、建库、建角色、复制或绕过行级安全权限。
- 两套 Compose 及所有联网迁移容器均配置 `host.docker.internal:host-gateway`。
- 固定使用已提供 PostgreSQL 支持的 `0.3.23.114` 镜像，并在每次演练/迁移开始时拉取、解析到镜像 ID；导入和首次启动使用同一镜像 ID。不运行旧镜像升级生产原库。
- `.database.env` 是目标机的运行状态文件。普通 SCM 发布首次创建空文件，已有文件原样保留；正式导入、校验通过后才写入 PostgreSQL 连接。此文件不由配置覆盖或旧文件清理，创建操作纳入事务日志。

## 只需发布一次，然后执行两种命令

### 1. SCM 下发

同步最新 conf，发布 `cpa-helper` 的 `hanada` 和 `huachienergy` 两个租户。目标机器需要 Docker Compose v2（已验证 2.20.3 及 5.5.1），已有 `postgresql` 容器和 Linux `python3`。脚本通过私有临时配置检查实际 env_file 读取能力，不依赖新版 `--no-env-resolution` 参数。

本次发布会沿用组件既有的 recreate 策略；在允许应用短暂重建的时间执行。下发脚本/凭据、网络映射和空的本机环境文件，**此时仍使用 SQLite**。凭据直接按 0600 下发，`post_config.sh` 不运行迁移。若已有容器使用 PostgreSQL 而连接文件缺失，发布会中止并要求恢复连接文件。

若只使用 SCM 的脚本上传功能，也可以把 `migrate-postgres.py` 作为 Python 脚本上传，执行参数保持下文一致。但两套安装目录必须先有上述 Compose 和 `postgres-migration.env`，脚本不会猜测现有部署或在缺少配置时停服。

### 2. 在线演练（不停止原应用）

SCM 选择目标主机、root 身份，逐条执行：

```bash
python3 /usr/local/cpa-helper/cpa-helper_hanada/tools/migrate-postgres.py rehearse --root /usr/local/cpa-helper/cpa-helper_hanada
python3 /usr/local/cpa-helper/cpa-helper_huachienergy/tools/migrate-postgres.py rehearse --root /usr/local/cpa-helper/cpa-helper_huachienergy
```

脚本创建各自专用用户（已由本脚本创建时保持密码不变）、随机后缀的独立演练库，通过 SQLite 在线备份获取包含 WAL 的一致快照，在副本上升级结构，依次执行导入、25 张业务表的行数/摘要/序列校验、`doctor`。**不停止现有应用、不切换配置、不启动第二个采集器**。

完成显示 `rehearsal-passed`。演练数据库与报告保留供核查；每次演练使用新库，不复用、不自动删除任何数据库。核查后由管理员按脚本输出的确切库名回收演练库即可，正式库不会带 `_test_` 后缀。

#### 快照超时或持续写入时

快照默认限时 1800 秒，可用 `--snapshot-timeout 3600` 延长；每 15 秒输出页数进度、耗时和进度重置次数。旧版固定 300 秒且没有进度信息，不能仅凭超时报错判断是磁盘慢还是持续写入导致反复备份。

- WAL 模式：固定只读事务快照，后续提交不会使备份反复重启，写入仍可继续。读快照释放前 WAL 可能增长，要保留足够磁盘空间。
- 非 WAL 模式（例如 `delete`）：保留分批在线备份，不强行更改数据库日志模式、不长期持有阻塞业务写入的读锁；持续写入仍可能使进度重置。此时不要仅无限延长超时。

允许短暂中断 CPA-Helper 页面、鉴权及采集服务时，可以显式加 `--stop-for-snapshot`：脚本正常停止原容器，取得一致快照后**立即恢复同一个 SQLite 容器及重启策略**，再升级副本、导入演练库；快照失败也尝试恢复。应用停止时间取决于停机和复制耗时，并非固定几秒；不停止上游模型请求时应确保采集队列能够保留暂停期间的数据。

```bash
python3 /usr/local/cpa-helper/cpa-helper_hanada/tools/migrate-postgres.py rehearse --root /usr/local/cpa-helper/cpa-helper_hanada --stop-for-snapshot --snapshot-timeout 3600
```

另一个实例替换路径中的 `hanada` 为 `huachienergy`。此选项仅适用于 `rehearse`；正式迁移原本就会停服，仍需先排空上游请求和采集队列。默认演练不会自动切换为停服模式。

如果旧脚本停在 `snapshot-sqlite` 后超时，该次尚未导入或切换生产数据库，原实例仍使用 SQLite。更新脚本后可以直接重新演练，新运行会使用新目录和新演练库；不需要删除上次报告、生产数据或 `.database.env`。

### 3. 维护窗口正式切换

先暂停该实例的模型入口/后台调用，等待在途请求结束、采集队列排空。脚本不能替你证明上游已经停写；`--traffic-paused` 是对这项前置条件的明确确认，不是绕过检查的通用 `--force`。

SCM root 身份执行（两套逐条执行，不要同时派发）：

```bash
python3 /usr/local/cpa-helper/cpa-helper_hanada/tools/migrate-postgres.py migrate --root /usr/local/cpa-helper/cpa-helper_hanada --traffic-paused
python3 /usr/local/cpa-helper/cpa-helper_huachienergy/tools/migrate-postgres.py migrate --root /usr/local/cpa-helper/cpa-helper_huachienergy --traffic-paused
```

脚本自动完成：

1. 校验安装路径、容器身份、镜像能力、Compose、数据库版本和网络；创建独立用户及**全新空库**，通过应用 Docker 网络验证真实密码连接。重试仅能复用本脚本创建且仍为空的库；非空库或未知库拒绝覆盖。
2. 关闭旧容器的自动重启，再等待其正常停止；若被强制结束，终止迁移并恢复原实例，要求核查在途数据。
3. 备份完整原数据目录及 Compose，生成单独 SQLite 副本，只升级副本。
4. 导入 → 独立二次校验 → `doctor`，任一步失败均不启用 PostgreSQL。
5. 原子写入权限 0600 的 `.database.env`（连接 URL、64 连接池），先检查 Compose 解析结果，再重建该实例，校验容器实际环境、数据库 `doctor` 和本机 `/api/ready`。
6. 输出 `completed` 和恢复资料目录。之后人工检查登录、配额/历史用量与一次受控请求，再放行业务流量。

SCM 任务超时要大于演练实测总耗时，并为全量备份/导入留余量；大库可设置 3 小时。不要让 SCM 在迁移期间并行发布或重启同一组件。脚本的主机锁只互斥本脚本，不是 SCM 全局部署锁。

## 失败、重试与恢复

- 报告和原始恢复点在 `<安装目录>/migration-backups/<UTC时间_随机后缀>/`，目录 0700。包括 `state.json`、原数据/配置（正式模式）、迁移副本、各阶段日志。里面含业务敏感数据，按生产备份保护。
- **启用 PostgreSQL 之前失败**：自动恢复原 SQLite 容器及原重启策略，保留目标库供检查。修复原因后，本脚本创建且仍为空的库可直接重试；已成功导入的非空库不会被自动清空，需先核查或在 conf 指定另一个新库名。不要无脑反复执行同一任务。
- **尝试启动 PostgreSQL 之后失败**：停止不确定状态的应用，不自动回退 SQLite，因为后台任务可能已产生新写入。保留 `.database.env` 和 PostgreSQL，优先修复当前 PG 实例；确认后用正常 Compose/SCM 部署恢复启动。
- **硬杀进程/主机断电**：先查看 `state.json`、`.database.env` 和容器实际连接。停服前已禁用原容器自动重启，防止原 SQLite 在迁移过程中意外恢复消费。不要根据“网页没打开过”推断 PG 没有新写入。
- `.database.env` 非空时脚本拒绝重复导入；首次发布创建的零字节空文件允许演练和迁移。迁移完成后请继续保留此文件，**不要删除它来尝试回退**。
- conf 中密码为既定凭据：重复演练不会旋转密码，也不会接管未知的现有数据库用户。需要改密时单独安排数据库与 conf/运行环境同步，不以本脚本改密。
- 每实例最多 64 个应用连接，两套合计最多 128；共享 PostgreSQL 还要给其他应用、迁移、备份和管理员留容量。脚本不擅自修改共享 PostgreSQL 的 `max_connections`、认证规则或防火墙。

更完整的边界和手工恢复流程见 [迁移与运维流程](postgresql-migration.md)。Compose 的 env_file 语义见 [Docker 官方说明](https://docs.docker.com/compose/how-tos/environment-variables/set-environment-variables/)。

## 脚本验证

```bash
python3 -m unittest discover -s scripts -p test_migrate_postgres.py -v
# 仅限有 Docker 的隔离测试机；创建和清理独立的合成容器/数据，不读取 conf 密码。
sudo CPA_MIGRATION_DOCKER_TEST=1 python3 scripts/test_migrate_postgres_docker.py
```
