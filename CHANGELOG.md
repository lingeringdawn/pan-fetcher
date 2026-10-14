# Changelog

## v2.0.0

### 破坏性变更
- **项目仅支持 Linux。** 移除了 Windows / macOS 支持：删除 `p115/login_darwin.go`、`login_windows.go`、`server/restart_windows.go`、`jackett/detach_windows.go` 与 `scripts/docker-setup.ps1`；平台后缀文件改名为 `login.go` / `restart.go` / `detach.go`，不再需要构建标签区分平台。
- **搜索后端统一为 Jackett**：移除内置 Cardigann YAML 索引引擎（`indexer/` 引擎、`indexers/` 定义目录、内置索引器自动安装）。所有搜索、RSS 聚合、订阅推送均通过 Jackett 的 Torznab API 完成。
- 移除索引器页的「本地库」区域及相关接口（`/indexers/test`、`/indexers/testall`、`/indexers/login`、`/indexers/edit`、`/indexers/delete`）。
- CI 只产出 Linux amd64 / arm64 产物；发布包不再提供 macOS 与 Windows。

### 新增
- **Jackett 自动检测**：启动时及索引器页面均可探测本机 Jackett。读取 `ServerConfig.json`（`$XDG_CONFIG_HOME/Jackett`、`~/.config/Jackett`、Docker 的 `/config/Jackett`），自动填充地址、API Key 与管理密码。已配置时不会被覆盖，也可用 `JACKETT_URL` / `JACKETT_APIKEY` 环境变量显式指定。
- **一键安装 Jackett**：检测不到本机实例时，可从索引器页面直接下载 Linux 发行包，解压到数据目录的 `jackett/` 子目录并启动，随后自动接管其配置。支持 amd64 / arm64 / arm；`JACKETT_MUSL=1` 选择 musl 构建（Alpine 容器已在 Dockerfile 中默认开启）。
- 解压过程对归档路径做穿越校验，含 `../` 或绝对路径的条目会直接判为非法并中止安装。
- `Start` 会返回实际应答的地址。Jackett 从用户配置目录而非安装目录读取端口，因此请求的端口未必生效，调用方必须采用返回的地址。
- 新增 `jackett/install_e2e_test.go`：用真实发行包验证下载、解压、可执行文件定位、启动与探测全链路，需 `JACKETT_E2E=1` 启用。
- 新增 `jackett/discovery_e2e_test.go`：让 Jackett 自行生成配置，验证自动检测读出的 API Key 能真正通过 Torznab 认证，而非仅断言字符串非空。
- 新增 `jackett/detach_test.go`：实际派生进程校验子进程分离标志，防止 `Setsid`/`Setpgid` 互斥问题回归。
- 新增 `jackett/client_test.go`：覆盖 Torznab 错误文档识别。
- 新增 `jackett/probe_test.go`：以假 Jackett 服务覆盖探测判据，含 TestCookie 重定向、空响应、以及拒绝同端口的普通 Web 服务。

### 修复
- 设置页改为「提交哪些字段就只改哪些字段」：此前一次部分提交（如自动检测只回填 URL）会把未提交的 Jackett API Key、TMDB Key 等一并清空。
- 一键安装的可执行文件名此前假设为 `Jackett` / `Jackett.exe`，实际 Linux 归档中是小写的 `jackett`，导致安装后无法启动。
- **API Key 校验形同虚设**：Jackett 对无效密钥返回的是 **HTTP 200 + `<error code="100" description="Invalid API Key" />`**，而非 401。原实现只检查状态码，因此填错 API Key 时：设置页「测试连接」会误报成功；索引器列表与搜索结果会静默显示为空，看起来像"没有索引器/没有资源"。现统一解析响应体识别错误文档，`Test`、`ListIndexers`、`ListAllIndexers`、`Search` 四处均生效。
- **子进程分离标志互斥（真实 Linux 上功能完全不可用）**：`SysProcAttr` 同时设置 `Setsid` 与 `Setpgid` 会让 `fork/exec` 返回 `EPERM`——`setsid(2)` 使子进程成为会话首进程，`setpgid(2)` 随即拒绝它。这导致一键安装启动 Jackett 与自更新重启均失败。现只保留 `Setsid`（它本身已创建新进程组），并新增回归测试实际派生进程验证。
- **Jackett 探测判据失效**：`/UI/Login` 返回 302 且响应体为空，而探测要求正文含 "jackett"，故永远判定失败，`Start` 会一直等到 90 秒超时。现同时识别 Jackett 特有指纹（`Set-Cookie: TestCookie`、`Server: Kestrel`、重定向至 `/UI/`），不再单纯依赖页面正文。
- **监听地址可能不可拨**：Kestrel 报告 `http://[::]:port`，但该地址常无法连接而 IPv4 回环可用。`Start` 现将日志中的监听地址与两种回环地址一并作为候选，并优先使用 IPv4。

### 清理
- 移除 Jackett 版本号探测。实测确认版本由页面 JavaScript 填入空占位符（`<span id="app-version"></span>`），HTML 中并不存在版本字符串，正则永远匹配不到；该逻辑曾额外发起两次 HTTP 请求却始终返回空值。相应的 `Discovery.Version` 字段、`extractVersion` 与界面上的版本显示一并删除。

## v0.3.1 (2026-07)

### 新增
- Jackett/Prowlarr 索引器集成：库管理、激活/停用、连接测试、批量操作
- 搜索结果来源标签（本地/Jackett）、冲突索引器标注
- 搜索列表底部分页栏：页码跳转、上一页/下一页、总数显示
- 可配置搜索分页大小（设置页，10–500，默认 50）
- RSS 订阅支持 Jackett-only 索引器搜索

### 优化
- 搜索引擎支持多页翻页（Page 参数传递到 YAML 模板）
- Jackett HTTP 客户端代理支持
- 搜索结果日期格式统一含年份：`2006-01-02 15:04`
- 刷新按钮清除 sessionStorage 防止切页回访残留
- 分页栏双语支持（中/英翻译键）

### 修复
- Jackett 搜索超时（proxy 未配置导致直连超时）
- Jackett-only RSS 订阅地址过滤失效（`jackett:` 前缀未剥离）
- 设置页布局重组：系统/下载/搜索/订阅与通知/Jackett 五组 fieldset
- 移除冗余的访问域名和界面语言配置（侧栏已有语言切换）
- Dockerfile 引用不存在的 config.example.toml

## v0.3.0 (2026-07)

### 新增
- Web 文件管理：新建/重命名/删除/移动，自定义弹窗交互
- 订阅管理整合缓存库：内嵌查看资源名、清空、折叠展开
- 侧栏语言切换（CN/EN）、关于页面
- 全局操作日志：FS/索引器/订阅/设置全覆盖
- 磁力链接 dn= 参数保留，缓存显示资源名而非 hash
- 订阅立即执行为 AJAX 免刷新，3 秒后自动刷新
- 顶栏公告区，状态消息移入 topbar

### 优化
- pageData 性能：连接检查缓存 60s，FS/设置页跳过任务加载
- 面包屑条目缓存（getEntryCached）
- 设置日志仅记录实际变更

### 清理
- 移除不生效的"跳过去重"（disable_cache）
- 删除失效索引器（kickasstorrent、skytorrents、dmhy_jackett）
- 删除空 webdav/ 目录、冗余 rsshub.go、/tasks 路由

## v0.2.3

### 新增
- 统一 config.toml 配置方案
- Linux systemd 安装脚本

## v0.2.2

### 修复
- 适配 elevengo 上游变更
- 新增 savepath 可选字段
