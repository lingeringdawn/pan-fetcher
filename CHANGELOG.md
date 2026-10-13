# Changelog

## Unreleased

### 变更
- **搜索后端统一为 Jackett**：移除内置 Cardigann YAML 索引引擎（`indexer/` 引擎、`indexers/` 定义目录、内置索引器自动安装）。所有搜索、RSS 聚合、订阅推送均通过 Jackett 的 Torznab API 完成，索引器定义的维护交由 Jackett。
- 移除索引器页的「本地库」区域及相关接口（`/indexers/test`、`/indexers/testall`、`/indexers/login`、`/indexers/edit`、`/indexers/delete`）。

### 新增
- **Jackett 自动检测**：启动时及索引器页面均可探测本机 Jackett。按平台读取 `ServerConfig.json`（Windows `%ProgramData%\Jackett`、Linux `~/.config/Jackett`、macOS `~/Library/Application Support/Jackett`、Docker `/config/Jackett`），自动填充地址、API Key 与管理密码。已配置时不会被覆盖，也可用 `JACKETT_URL` / `JACKETT_APIKEY` 环境变量显式指定。
- **一键安装 Jackett**：检测不到本机实例时，可从索引器页面直接下载对应平台的 Jackett 发行包（Windows zip、Linux glibc/musl、macOS），解压到数据目录的 `jackett/` 子目录并启动，随后自动接管其配置。支持 `JACKETT_MUSL=1` 选择 musl 构建。
- 解压过程对归档路径做穿越校验，含 `../` 或绝对路径的条目会直接判为非法并中止安装。

### 修复
- 设置页改为「提交哪些字段就只改哪些字段」：此前一次部分提交（如自动检测只回填 URL）会把未提交的 Jackett API Key、TMDB Key 等一并清空。

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
