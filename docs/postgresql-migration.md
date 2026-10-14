# SQLite → PostgreSQL 迁移与运维流程

本文适用于包含 PostgreSQL 支持的 CPA-Helper 版本。**不会自动迁移旧数据**：仅设置连接地址并启动，会在空库里初始化一套新系统。已有用户必须按下面的停机导入流程操作。

**已有本机 `postgresql` 容器、通过 SCM 发布的部署，优先使用 [SCM 一键演练与正式迁移](postgresql-scm-quickstart.md)**：单文件 Python 脚本自动准备专用数据库/用户、快照、导入、校验与切换；应用使用 `host.docker.internal:63851`、64 连接池。本文以下保留通用手工流程，不需要再逐条重复执行。

## 1. 本次支持范围

- 不设置 `CPA_HELPER_DATABASE_URL`：继续使用原 SQLite 文件，不改变已有部署行为。
- 设置该变量：使用 PostgreSQL / pgx 连接池，默认最多 16 个连接，可通过 `CPA_HELPER_DB_MAX_CONNS` 设置为 2–100。
- PostgreSQL 15+；本地集成验证使用 PostgreSQL 15。Compose 示例使用 PostgreSQL 17，请先在实际目标版本演练。
- PostgreSQL 使用独立的内嵌 goose 迁移链，当前基线与 SQLite 的 `202609280001` 等价；后续结构变更需同时维护两种数据库迁移。
- 迁移保留全部 25 张业务表：用户和密码、CAS/OIDC 绑定与会话、API 密钥、设置、价格与节假日、教程、凭证巡检、明细、小时聚合、永久去重账本、配额、额度卡、重置及扣款流水等。
- 历史费用直接复制，不按当前价格重新计算；保留原始 ID、时间文本、空值、自增序列高水位（包括已删除的 ID）。金额在 PostgreSQL 使用 NUMERIC，应用层计费仍遵循原来的浮点数及 8 位小数规则。
- 同一用户的扣费、卡片修改/吊销和重置使用用户行锁。聚合与入库使用相同的水位锁，避免 ID 先分配后提交造成遗漏；混合聚合查询使用可重复读快照。
- **仍然只运行一个 CPA-Helper 服务实例**。本次不是多副本/HA 改造：采集、巡检和远端密钥同步仍由进程内任务协调。不要让新旧实例同时消费同一队列。
- 没有在线双写、增量同步或 PostgreSQL → SQLite 反向转换工具。

## 2. 准备与演练

1. 记录旧镜像/二进制版本、Compose 文件、环境变量、数据卷、网关配置和当前队列名称。保存旧镜像以备回滚。
2. 在隔离数据库先演练一次导入，测量耗时和磁盘占用。**演练时不要启动带真实 CPA 配置的服务**，否则可能采集生产消息或修改真实凭证/密钥。
3. 备份整个 `data/` 以及部署配置。SQLite 正在运行时，不能只复制主文件：使用 SQLite 在线备份；正式切换推荐停止进程后复制整个数据目录（包括存在的 WAL/SHM 文件）。
4. 确认源库结构已是当前版本。若版本旧，在停机且备份后，用新二进制在 **SQLite 模式**运行 `migrate`。不要对唯一备份升级；保留升级前原件和升级后的迁移副本。
5. PostgreSQL 使用 UTF-8、独立数据库和专用非超级用户。准备足够容纳数据、索引、事务 WAL 与备份的磁盘空间。导入是单事务，不能暂停后续传；耗时须以演练为准。
6. 除同机回环或受控容器网络外，连接必须配置 TLS（推荐 `sslmode=verify-full&sslrootcert=...`），不要在公网使用示例里的 `sslmode=disable`。连接 URL 中的密码需百分号编码；不要提交含密码的环境文件、日志或报告。
7. 本程序需要表/索引/序列创建权限（启动迁移），无需超级用户或 CREATEDB 权限。测试套件的 CREATEDB 权限不适用于生产账号。

## 3. Docker Compose 完整切换示例（Linux、沿用 host 网络）

下面沿用仓库 `docker-compose.yml` 的 host 网络，使原 CPA 地址和网关端口保持不变。Docker Desktop/Windows 或 bridge 部署需要按实际网络修改 DSN，不能照搬 host 网络假设。

### 3.1 准备新镜像和 PostgreSQL

在原部署目录放入本版本的 `docker-compose.postgres.yml`。创建权限受限的 `.env.postgres`（自行填真实值，不要提交）：

```dotenv
CPA_HELPER_IMAGE=registry.hanada.info/hanada/cpa-helper:0.3.23.113
POSTGRES_ADMIN_PASSWORD=填入独立的管理员强密码
CPA_HELPER_DATABASE_URL=postgres://cpa_helper:填写URL编码后的应用密码@127.0.0.1:55432/cpa_helper?sslmode=disable
CPA_HELPER_DB_MAX_CONNS=16
```

镜像标签必须确认已发布，不能仅根据源码版本号推断构建成功。拉取镜像并只启动数据库：

```bash
chmod 600 .env.postgres
docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml pull
docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml up -d postgres
docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml exec postgres psql -U postgres -d postgres
```

在 psql 中创建专用角色和空库；用交互式密码输入避免把密码写入 SQL 历史：

```sql
CREATE ROLE cpa_helper LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
\password cpa_helper
CREATE DATABASE cpa_helper OWNER cpa_helper TEMPLATE template0 ENCODING 'UTF8';
\connect cpa_helper
REVOKE CREATE ON SCHEMA public FROM PUBLIC;
GRANT USAGE, CREATE ON SCHEMA public TO cpa_helper;
\q
```

**目标库 public schema 必须为空。不要先对目标库运行 `migrate`，不要先启动新应用。**

### 3.2 关闭写入入口、停旧服务并备份

1. 网关进入维护状态，暂停新的模型请求及后台生产任务；等待在途请求结束。
2. 让旧采集器消费完剩余用量并确认没有采集错误，再停止旧 CPA-Helper。若不能完全停上游流量，必须确认 CPA 队列会可靠保留停机期间的消息及容量/TTL；不能在未经确认时承诺零丢失。
3. 禁止用户访问旧/新管理页面；停止其他维护脚本。停止前若采集器已从上游弹出但尚未落库的消息丢失，本迁移工具无法恢复，必须在业务停写后确认队列与在途处理已清空。

```bash
set -eu
docker compose stop -t 120 cpa-helper
# 已有备份时停止，不覆盖上次切换的恢复点；请另选备份目录。
test ! -e backups/sqlite-before-pg
test ! -e backups/sqlite-current
mkdir -p backups/sqlite-before-pg backups/sqlite-current
chmod 700 backups backups/sqlite-before-pg backups/sqlite-current
cp -a data/. backups/sqlite-before-pg/
cp -a data/. backups/sqlite-current/
```

如源库需升级，对 `backups/sqlite-current` 这个副本运行新版本 SQLite 迁移，不操作原备份：

```bash
docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml run --rm --no-deps \
  -e CPA_HELPER_DATABASE_URL= -v "$PWD/backups/sqlite-current:/app/data" \
  cpa-helper /app/cpa-helper migrate
```

该命令只迁移，不启动采集/巡检。备份文件包含密钥、密码哈希、会话密钥与 OIDC 信息，应设置访问权限并加密存放。

### 3.3 导入与二次校验

```bash
docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml run --rm --no-deps \
  -v "$PWD/backups/sqlite-current:/migration:ro" \
  cpa-helper /app/cpa-helper import-sqlite --source /migration/db/cpa_helper.sqlite3

docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml run --rm --no-deps \
  -v "$PWD/backups/sqlite-current:/migration:ro" \
  cpa-helper /app/cpa-helper verify-sqlite --source /migration/db/cpa_helper.sqlite3

docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml run --rm --no-deps \
  cpa-helper /app/cpa-helper doctor
```

检查每条命令退出码必须是 0，并保存 25 张表的行数/SHA-256 报告。导入会：

1. 只读检查源库版本、完整性、外键及表集合。
2. 拒绝非空目标，创建新表并按外键依赖顺序批量复制。
3. 对每张表按主键排序，比较行数与规范化内容 SHA-256（数值按应用的浮点表示规范化，文本原样）。
4. 恢复序列高水位，写入 PostgreSQL 自己的 goose 版本记录。
5. 所有检查通过才提交；任一步失败，目标 schema 和数据一起回滚。
6. 提交后运行 ANALYZE。若仅 ANALYZE 失败会明确打印警告：导入已经成功，修复连接后补跑 ANALYZE，不要重试导入非空库。

导入并不关闭配置里的采集开关，也不修改会话密钥；因此必须在数据核对完成后才启动服务。两次导入不允许覆盖，已有空白初始化库也会被拒绝：应由管理员创建另一个空库重试，工具不会代为清库。

### 3.4 启动与验收

```bash
docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml up -d cpa-helper
docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml logs --tail=100 cpa-helper
curl --fail http://127.0.0.1:18317/api/ready
```

只放行管理员，逐项确认：

- 本地应急登录 `?skipsso=true`、CAS/OIDC（用真实 IdP 单独验收）、管理员与普通用户权限。
- 用户、密钥、配额/额度卡、价格档位、教程、凭证列表一致。
- 用量分析历史总数、费用、Token 与迁移前截图/报告一致；请求详情、小时聚合与卡片抵扣正确。
- 仅发起一次受控模型请求，确认新明细出现、费用固定、扣款恰好一次。
- 正常采集/巡检恢复，没有 SQL、锁等待或数据库连接错误。

通过后再恢复业务流量。**服务一旦启动，即使尚未放行用户，采集/保留清理/会话等后台任务也可能写入；从此不能直接切回旧 SQLite。**

## 4. Windows 二进制等效流程

同样先停止服务与业务写入，备份整个 data 目录，准备专用 PostgreSQL 角色/空库。以下示例以已备份的迁移副本为源：

```powershell
# 新二进制的完整路径和迁移副本目录按实际修改。
$env:CPA_HELPER_DATA_DIR = 'D:\cpa-migration\sqlite-current'
$env:CPA_HELPER_DATABASE_URL = ''
& 'C:\CPA-Helper\cpa-helper.exe' migrate
if ($LASTEXITCODE -ne 0) { throw 'SQLite upgrade failed' }

# 从受保护的配置设置真实 DSN，不要把密码贴到聊天或提交到 Git。
$env:CPA_HELPER_DATABASE_URL = 'postgres://cpa_helper:URL_ENCODED_PASSWORD@127.0.0.1:5432/cpa_helper?sslmode=disable'
$env:CPA_HELPER_DB_MAX_CONNS = '16'
& 'C:\CPA-Helper\cpa-helper.exe' import-sqlite --source 'D:\cpa-migration\sqlite-current\db\cpa_helper.sqlite3'
if ($LASTEXITCODE -ne 0) { throw 'Import failed; do not start service' }
& 'C:\CPA-Helper\cpa-helper.exe' verify-sqlite --source 'D:\cpa-migration\sqlite-current\db\cpa_helper.sqlite3'
if ($LASTEXITCODE -ne 0) { throw 'Verification failed' }
& 'C:\CPA-Helper\cpa-helper.exe' doctor
if ($LASTEXITCODE -ne 0) { throw 'Readiness check failed' }
```

把 DATABASE_URL 和 DB_MAX_CONNS 写入服务管理器的环境配置后再启动，不能只在当前 PowerShell 临时设置。不要把应用的数据目录改成唯一备份目录；运行路径与迁移副本分开保存。

## 5. 失败处理与回滚

| 阶段 | 处理 |
| --- | --- |
| 导入尚未成功 | 目标事务自动回滚；修复源完整性/字段/网络问题后对空库重试。源文件没有被导入工具修改。 |
| 导入成功，且尚未启动/产生任何新写入 | 可取消切换，移除 DATABASE_URL，使用原镜像和升级前原数据目录重启 SQLite。目标 PG 留存调查，不必删除。 |
| PostgreSQL 已开始新写入 | 先停业务和应用，备份当前 PG。优先修复后继续使用 PG，或恢复经过验证的 PG 备份/PITR。旧 SQLite 不包含新用量/扣款，直接切回会丢账，禁止把它当成无损回滚。 |
| 非空目标拒绝导入 | 不要自动删除表；确认是否误连生产库。创建新的独立空库后重试。 |
| 源版本不一致 | 不修改版本号冒充升级；对副本运行同版本 SQLite migrate 后再导入。 |

本版本没有反向增量回迁工具。如果业务要求写入后也能回退 SQLite，需要另行规划、验证逆向数据同步；不能依赖旧备份。

## 6. PostgreSQL 后续备份、恢复和性能

同机 Compose 备份（在容器内写文件，避免 Windows PowerShell 重定向破坏二进制 dump）：

```bash
docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml exec postgres \
  pg_dump -U postgres -d cpa_helper -Fc -f /tmp/cpa-helper-backup.dump
docker compose --env-file .env.postgres -f docker-compose.yml -f docker-compose.postgres.yml cp \
  postgres:/tmp/cpa-helper-backup.dump ./backups/cpa-helper-backup.dump
```

dump 不包含角色密码，角色与部署配置需另行安全备份。恢复演练应创建另一空库，使用 `pg_restore --exit-on-error --no-owner -U postgres -d 新库 --role=cpa_helper 备份文件`，先执行 doctor，再对历史费用、配额、去重与样本数据验收。**不要把演练库启动为连接生产 CPA 的第二个采集器。**需要低 RPO 时另外配置 WAL 归档/PITR 并实测恢复。

维持 autovacuum，监控连接数、锁等待、长事务、磁盘/WAL、慢查询与 pg_stat_user_tables。不要一开始就把连接池调到 100：先用 16，再根据并发与数据库资源调整。连接数要为备份、运维和其他应用留余量。

迁移本身不保证固定倍数提速，尤其是低并发查询或仍需 Go 聚合的历史表盘。上线前后使用相同数据量、请求和并发对比 p50/p95、数据库 CPU/IO、锁等待、采集积压以及错误率。

## 7. 开发验证

```powershell
cd backend
go test ./... -count=1
# 专用测试 PostgreSQL，角色需要 CREATEDB；每个测试创建并删除随机 cpa_test_* 数据库。
$env:CPA_HELPER_TEST_POSTGRES_URL = 'postgres://test_user:password@127.0.0.1:5432/postgres?sslmode=disable'
go test ./internal/app -run '^TestPostgres' -count=1 -v
```

TEST_POSTGRES_URL 不会替换生产配置；测试只删除自己创建的随机数据库，不删除 URL 所指的数据库。没有设置测试 URL 时 PostgreSQL 测试会明确跳过。可选的 `scripts/postgres-test-server.sh` 在 Debian/WSL 临时目录解包测试 PostgreSQL 15，只监听回环地址、使用 trust 认证，**仅限可信单用户测试机，不用于生产**；结束后按脚本输出停止服务。
