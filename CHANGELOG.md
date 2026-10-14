# Changelog

## v2.0.7

### 修复
- **自更新后面板重置为无密码**：登录密码在数据目录锚定之前读取，于是按进程工作目录查找 `web-settings.json`。自更新重启会把工作目录改为二进制所在目录，文件找不到，服务便以空密码启动——**整个面板静默失去认证**。现新增 `server.InitDataDir()`，在读取任何配置之前锚定数据目录，启动顺序同步调整。
- **运行日志顺序错乱与重复**：面板按最旧在上渲染，客户端却用最旧一行作轮询游标，服务端从最新端匹配后返回其后几乎全部历史，每 5 秒整页追加一次。现统一为**最新在上**，增量块直接前插；游标不在缓冲区时返回 `reset` 标记，客户端替换内容而非前插，避免整页重复。
- **索引器库列表排版错乱**：存在两个同名 `renderJKAddList` 定义，后者覆盖前者但调用点行为混乱；分页实现又把「加载更多」按钮放在 `</tbody></table>` 之后，追加时用正则剥掉 `<tbody>` 之前的全部内容，连闭合标签一并删除，表格结构因此破损。现改为表壳只建一次、分页只向固定 `tbody` 追加行、按钮独立于表格之外，并防止按钮重复绑定。
- **Jackett 启动失败被误报为成功**：启动后仅记录 pid 即认为成功，不检查进程是否存活。缺少 `libicu` 时 .NET 会在一两秒内中止，界面却显示「安装成功」，随后空等 90 秒。现启动后确认进程存活（含 zombie 判定），失败时从日志首行提取原因，并把 ICU 缺失翻译为可操作的安装提示。

## v2.0.6

### 修复
- **索引器库报 JSON 解析错误**：部分接口用字符串拼接构造 JSON 响应，把错误信息直接插进 JSON 字符串里。错误信息含引号时（例如 `Get "http://...": context deadline exceeded`）会提前终止字符串，响应体不再合法，界面只能显示 `Expected ',' or '}' after property value in JSON at position 43` 这类解析错误，而看不到真正的原因。现改为统一经 JSON 编码输出，共修正 9 处（Jackett 索引器列表与添加、连接测试、离线任务提交、二维码登录、目录浏览、订阅执行、批量启停）。

## v2.0.5

### 修复
- **索引器库加载缓慢**：「添加库」弹窗此前一次性请求并渲染 Jackett 返回的全部索引器（通常有数百个），索引器多时打开缓慢、页面卡顿。现改为分批渲染：每次 50 条，其余通过「加载更多」逐次追加，打开后立即可见首批结果。
- **添加索引器失败（超时）**：向 Jackett 添加索引器需三步——登录、读取默认配置、写回配置。三步此前共用一个 60 秒总时限，而 Jackett 在配置变更后会重新加载并测试全部索引器，期间无法响应请求；前面的步骤耗尽整个时限，导致最后一步写配置超时。现每步独立计时，并在已持有登录会话时跳过重复登录（此前每次调用都会重新登录，额外增加一轮往返）。
- **添加成功后页面刷新**：此前每添加一个索引器都会刷新整页并重新拉取全部索引器列表。现仅更新该行状态，弹窗保持原位，可继续添加下一个。

## v2.0.4

### 修复
- **RSS 订阅搜索超时且结果为空**：`jackett.Search` 未设 `limit`，Jackett 于是全量查询所有已配置索引器并返回全部结果。实测 `q=TARI TARI` 耗时 **19.5 秒**返回 **1185 条**，恰好卡在 20 秒请求超时的边界上，偶发超时后订阅取不到任何结果。加上 `limit=100` 后同一查询 **2.4 秒**返回 100 条。该 limit 与面板翻页步进取同一常量，避免两处漂移。
- **请求超时上限偏紧**：`jackett` 包内三处 context 硬编码 20 秒，而 HTTP client 本身是 60 秒，context 反而成了更紧的那道闸。现统一为 `requestTimeout = 60s`。
- **无代理时使用无超时的 HTTP 客户端**：`server.httpClient()` 在未配置代理时返回 `http.DefaultClient`（无超时），配置了代理反倒有 90 秒超时。现两种情况都使用带 60 秒超时的客户端。

## v2.0.3

### 修复
- **索引器页面加载不出来（有激活索引器时）**：`jkDeactivate` 按钮与 `updateBrowseModal` 函数定义在 `{{range .IndexerList}}` 内部，此处的 `.` 是 `IndexerInfo`，而它们引用了 `{{index .T ...}}` —— `IndexerInfo` 没有 `T` 字段，模板执行到该行即报错中止。由于 `handleIndexers` 丢弃了 `Execute` 的返回值，已写入的部分被保留、错误被静默吞掉，表现为页面在 `title="` 处被硬截断，其后内容（含整个 `<script>` 块）全部丢失，于是 `jk-setup-body` 永远停在"加载中…"，索引器一个也加载不出来。索引器列表为空时该分支不执行，所以页面看起来正常。现改为 `$.T` 指向顶层数据。
- **运行日志无限重复**：`/log` 页面为最旧在上，客户端却用**第一行**（即最旧一行）作为轮询游标；`/api/logs` 从最新端向后匹配该游标，于是返回其后几乎全部历史，客户端每 5 秒把整页日志再追加一遍。同时 `/api/logs` 与页面返回的都是倒序，客户端直接 append 导致顺序颠倒。现统一为最旧在上，游标取**最后一行**（最新），增量返回其后的新行。

### 内部改进
- `handleIndexers` 等 17 处中的其余位置此前已检查 `Execute` 返回值，此处补齐同类问题的可见性：新增回归测试直接执行模板并在出错时失败，避免"截断但无日志"再次发生。
- `/api/logs` 的 `since` 改为精确整行匹配（原先用 `strings.Contains`，日志内容含游标串时会误匹配），找不到游标时回退全量而非静默返回空。

## v2.0.2

### 修复
- **自更新后 Jackett 连接丢失**（根因）：自更新与手动重启都会把子进程的工作目录设为二进制所在目录（`cmd.Dir = filepath.Dir(exe)`）。工作目录一变，`config.toml` 便不再被找到，数据库路径退回到二进制旁的默认值，数据目录随之整体迁移——`web-settings.json` 读不到，Jackett 的地址与 API Key 变成空值，表现为"更新后连接异常"。现重启时保持原工作目录，并将所有运行时文件锚定到数据库所在目录。
- **`compareVersion` 误判预发布版本**：解析用 `strconv.Atoi` 且忽略错误，`"1-rc1"` 变成 0，导致 `v2.0.1-rc1` 与 `v2.0.0` 比较相等，更新提示被隐藏。现理解 `-rc`/`-beta` 等后缀，正式版高于其自身预发布版；无法解析的输入不再伪装成合法版本。

### 变更
- **「自动检查更新」开关不再形同虚设**：此前该选项只被存取与显示，全项目无任何代码消费它。现勾选后启动后台检查协程，按订阅间隔轮询，发现新版本时记录日志并推送通知。仍**不会自动安装**——应用更新会重启服务并中断 RSS 轮询与在途下载，因此保留为设置页的手动操作。文案同步改为「自动检查更新并通知」。
- 权限不足时的错误提示改为可操作：此前直接透出 `write: open ...: permission denied`，现指出具体目录并说明解决办法（以管理员身份执行，或调整目录权限）。非权限类错误仍保留原始错误信息。

### 清理
- 移除 `copyFile()`：零调用，原为 Windows 自替换方案准备。
- 移除前端 `j.action==='sudo'` 分支与 `update_need_sudo` 文案：后端从不返回 `action` 字段，该分支永不触发，配套文案因此成了孤儿。

### 测试
- 新增 `server/update_test.go`：`compareVersion` 全序（含预发布、空版本、垃圾输入）、权限与非权限错误提示、`resolveDataFile` 不受工作目录影响（自更新配置丢失的回归测试）、绝对路径保持原样、`notify.SetDataDir` 生效。

## v2.0.1

### 修复
- **Linux 一键安装脚本指向旧仓库**：`scripts/install-release.sh` 的 `REPO` 仍是重命名前的 `mguyenanastacio-glitch/pan-fetcher`，导致 release 查询与二进制下载依赖 GitHub 的重定向才不失败；文档示例中的分支也写成不存在的 `main`（仓库默认分支为 `master`），照抄会 404。现改为正确的仓库名并抽出 `BRANCH` 变量。
- **安装脚本尝试下载已删除的索引器目录**：`install_indexers()` 会从仓库拉取 `indexers/*.yml`，而该目录已在 v2.0.0 随内置引擎一并移除，请求全部 404。该函数及调用已删除。
- **Docker 镜像名指向不存在的仓库**：`docker-compose.yml` 使用 `mguyenanastacio/pan-fetcher:latest`（Docker Hub 上不存在，拉取返回 401）。现改为实际存在的 `lingeringdawn/pan-fetcher`；`scripts/docker-setup.sh` 下载 compose 文件的地址同样由旧组织名更正。

### 变更
- **Docker 部署内置 Jackett 服务**：`docker-compose.yml` 新增 `jackett` 服务（`linuxserver/jackett`）。v2.0.0 起搜索完全依赖 Jackett，仅启动 pan-fetcher 会让搜索不可用。两个容器共享配置目录，pan-fetcher 借此读取 `ServerConfig.json` 自动获得 API Key，无需手工填写；`JACKETT_URL` 显式指向服务名（容器内无法通过端口探测发现它）。

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
