# CAS / OIDC 单点登录

配置入口：**系统设置 → 认证配置**。CAS 与 OIDC 可以同时开启，使用同一套本地用户、角色、API 密钥、配额和用量记录，不需要新建第二套账号。

## 升级已有 CAS 部署

- OIDC 默认关闭，不会自动修改已有 CAS 服务地址。
- 数据库迁移将原“默认使用 CAS 登录”转换为“强制登录方式：CAS”，保留原行为。
- 先完成 OIDC 配置并通过手动登录验证账号互通，再将“强制登录方式”切换为 OIDC。
- 保留一个有本地密码的管理员，并确认 `/login?skipsso=true` 可以使用。该入口不会跳转到 CAS 或 OIDC。

## 配置 OIDC

在身份服务中注册一个使用授权码流程的 Web 客户端，支持 PKCE S256，并授权 `openid profile email` scope。

以平台地址 `https://gateway.example.com` 为例，在身份服务中登记：

| 用途 | 地址 |
| --- | --- |
| 登录回调 | `https://gateway.example.com/oidc/callback` |
| 退出回调 | `https://gateway.example.com/oidc/logged-out` |

在认证配置中填写：

| 配置项 | 说明 |
| --- | --- |
| OIDC Issuer | 身份服务公布的 Issuer，必须与 Discovery / ID Token 中的 issuer 一致，不是授权地址 |
| OIDC 公网地址 | 当前平台的浏览器访问地址，例如 `https://gateway.example.com` |
| Client ID | 身份服务分配的客户端 ID |
| Client Secret | 客户端密钥；保存后不回传浏览器，编辑时留空表示保留原值 |
| 用户名字段 | 默认 `preferred_username`；必须是与 CAS 用户名一致的 claim |
| OIDC 退出地址 | 可选；留空使用 Discovery 的 `end_session_endpoint` |
| OIDC 自动创建普通用户 | 允许未登记用户首次登录时创建普通账号，配额使用下方“新用户默认配额” |

Issuer、公网地址使用 HTTPS，本机调试可使用 `localhost`、`127.0.0.1` 或 `::1` 的 HTTP 地址。认证端点自动从 Discovery 读取。公网地址填写站点根地址，不要追加登录回调路径。

反向代理需要将 `/oidc/` 与现有 `/cas/`、`/api/` 一样转发给 CPA-Helper，不应对回调做额外登录拦截或缓存。生产部署应保持浏览器访问地址与配置的公网地址一致。

## CAS 与 OIDC 用户互通

首次关联以**准确一致的用户名**为依据，关联之后以 **Issuer + Subject (`sub`)** 识别 OIDC 身份：

1. 原账号已经通过 CAS 登录：OIDC 配置的用户名字段与 CAS 用户名一致时，关联到原用户 ID，保留管理员角色、昵称、密钥、已用配额、额度卡及历史数据。
2. OIDC 首次创建用户：后续使用相同用户名通过 CAS 登录，也会关联同一用户 ID。
3. 原账号只是同名本地账号，尚未绑定 CAS：不会仅凭名称自动关联 OIDC。先通过已有 CAS 登录完成绑定，再使用 OIDC。
4. 同一用户已绑定不同的 OIDC 身份：拒绝自动覆盖或合并。
5. OIDC 绑定后，即使用户名 claim 改变，仍以原 Issuer + Subject 登录原用户，不自动重命名或迁移账号。

不要把昵称或不稳定的展示名称当作关联字段。身份服务如果使用 `name` 表示登录用户名，只有确认其与 CAS 返回值一致时才选择 `name`。缺少配置字段时尝试 UserInfo，并校验 UserInfo 的 `sub`；仍缺失则拒绝登录，不猜测其他字段。

已禁用的本地账号不能通过任一方式登录。新建用户不会因单点登录自动取得管理员权限；已有权限由本地用户管理决定。绑定关系不提供自行解绑入口。

## 强制登录方式与应急入口

“强制登录方式”可选“不强制 / OIDC / CAS”，对应方式必须先启用。

- 不强制：登录页提供本地密码以及已开启的 CAS / OIDC 按钮。
- OIDC 或 CAS：访问登录页时直接进入所选身份服务，不先显示密码表单。
- `/login?skipsso=true`：始终保留本地密码及手动 SSO 按钮，行为与原 CAS 应急入口一致。
- 这里沿用原来的自动跳转语义，不会封禁本地登录 API 或另一种已开启的 SSO。
- 选择自动登录后，不允许在个人资料中修改本地密码，仍应在统一账号中心管理认证凭据。

## 会话与退出

- 平台登录会话有效期为 7 天。
- OIDC 登录和退出流程状态有效期为 5 分钟，一次使用后失效，并绑定发起浏览器。
- 登录校验授权码、PKCE、state、nonce、Issuer、Audience、签名和有效期。
- OIDC 身份令牌在服务端加密保存，仅用于发起该会话的身份服务退出；访问令牌不持久化。
- OIDC 退出先撤销本地会话，再进入发现或配置的退出端点。
- 退出完成后回到 `/login?skipsso=true`，避免自动登录循环。
- 身份服务没有退出端点时，只退出本平台，并在登录页提示需到身份服务退出后再切换账号。
- CAS 登录仍使用 CAS 退出；本地密码登录仅退出本地会话。

客户端密钥属于服务端配置，应保护数据库与备份文件；“不回传浏览器”不代表数据库中的客户端密钥已加密。会话令牌加密依赖现有 Session Secret，轮换会使原平台会话及其 OIDC 退出令牌失效。

## 验证范围

自动化测试使用本地模拟身份服务验证 Discovery、签名、PKCE、UserInfo、账号互通、迁移、应急入口与退出流程。上线时仍需使用实际身份服务检查 claim 映射、客户端 scope、登录/退出回调白名单及反向代理路由。

实现参考本工作区 `scm-platform` 的 OIDC 流程，协议说明：

- [OpenID Connect Core：ID Token 验证](https://openid.net/specs/openid-connect-core-1_0.html#IDTokenValidation)
- [OpenID Connect RP-Initiated Logout](https://openid.net/specs/openid-connect-rpinitiated-1_0.html)
- [go-oidc](https://github.com/coreos/go-oidc)
