<img src="assets/icon.svg" width="96" alt="日历笔记图标">

# 日历笔记

[DBX](https://dbxio.com) 的月视图日历。每一天都带农历、节气或节假日安排，双击任意格子即可记录备忘。

[English](README.md)

## 功能

**日历网格**

- 月视图，顶部带星期栏，按周一起始
- 周六周日整列淡色底纹，相邻月份的日期淡化显示
- 上/下月切换与「今天」按钮
- 今天的格子带描边与底色
- 网格铺满面板高度，内部滚动

**每日格子**

- 日期数字右侧一栏，按优先级取值：**农历节日 → 法定节假日 → 节气 → 农历日期**
- `休` / `班` 角标，区分法定放假与调休上班
- 多日假期在当天显示节名，窗口内其余各天显示当天农历

**备忘**

- 双击格子打开编辑框
- `Ctrl`/`Cmd` + `Enter` 保存，`Esc` 关闭，同一个编辑框内可清除已有内容
- 内容通过 Go sidecar 持久化，存放在宿主的插件数据目录下

**导出与导入**

- 工具栏菜单可把备忘导出为 `.xls` 或 `.txt`，范围可选全部、近 1/3/6/12 个月或自定义起止
- 导出对话框上显示输出地址并可修改，旁边有复制路径与打开文件夹按钮
- 导入读取 `.xls` 或 `.xlsx` 文件的第一个 sheet，每行一个日期加一条内容
- 日期列兼容文本、真实日期单元格，以及 Excel 序列号三种形态
- 首列不是日期的行（表头、空行）会被跳过
- 文件与日历都有内容的日期会统计为冲突，可选覆盖、跳过或合并
- 导入是一次调用替换整个备忘集，因此失败不会留下写了一半的结果

**与宿主集成**

- 跟随 DBX 宿主的语言设置，中文或英文
- 使用宿主的 `Canvas` / `CanvasText` 主题色

## 环境要求

- DBX `>=0.6.0`，Host API `1`
- UI 需要 Node.js，sidecar 需要 Go 1.22，开发需要 `dbx-plugin` CLI

## 开发

```bash
npm install
dbx-plugin dev --path . --port 5190
```

`src/` 下的源码由 Vite 编译到 `ui/`，宿主加载的是 `ui/`；`backend/` 编译为 sidecar 二进制。最终集成测试请用真实的 DBX 宿主。

## 架构

插件由两半组成，通过 DBX 插件协议通信：

- **`src/` —— Svelte UI**：渲染月视图网格与编辑框。农历、24 节气、传统节日在本地计算，因此格子的渲染不等 sidecar。
- **`backend/` —— Go sidecar**：负责唯一需要跨重启存活的数据，即备忘。

分工依据是数据的性质：可确定性计算的东西留在 UI，需要存储的东西走后端。

`src/dbx.js` 是唯一接触宿主桥接的模块。桥接不存在时（例如直接用浏览器打开页面），日历仍能用本地计算的数据渲染，而写入备忘会明确报告无法保存。

### 备忘存在哪里

宿主会给每个 sidecar 注入 `DBX_PLUGIN_DATA_DIR`。备忘存放在该目录下的 `notes/<命名空间>.json`，写入是原子的（临时文件 + rename），因此写入被中断不会损坏文件。解析失败的文件会被改名保留为 `*.corrupt-<时间戳>` 而不是删除——备忘是不可再生的数据。

命名空间在宿主按连接寻址时取连接 id，否则取固定字符串 `workbench`。没有数据目录时 sidecar 仍能运行，但写入备忘会返回明确错误而不是假装成功，UI 会把该错误显示出来。

### 导出的文件落在哪里

宿主把 UI 渲染在 **opaque origin 的 sandbox iframe** 里，而缺少 `allow-downloads` 的 frame 会**静默丢弃**对 blob URL 的 anchor 点击：不抛异常，也不产生文件。所以这条路在 DBX 里根本走不通，只剩 sidecar——它是普通进程，能把文件放到用户指定的任何地方。

导出对话框上直接显示输出地址，用户可改：

- 初始地址是**上次导出到的文件夹**，第一次则用用户的下载目录。这个目录记在数据目录下的 `export-dir`。
- **选择…** 打开系统「另存为」对话框，目录和文件名都由用户定。返回值原样使用；操作系统已经就覆盖问过用户，所以那个文件会被替换。
- 不是用户手选的地址**绝不覆盖**同名文件：重名会变成 ` (2)`、` (3)` 递增。
- **复制路径**和**打开文件夹**作用于同一个地址，后者会在资源管理器里选中该文件。

写盘失败会如实报错，不会被当成导出成功。在宿主之外打开页面时没有 sidecar 可写，普通浏览器标签页退回浏览器自身的下载。

系统「另存为」、在资源管理器里定位、剪贴板这三件事都走 Windows shell，因此**这三项功能仅限 Windows**；导出本身是可移植的。

## 构建与发布

```bash
npm run build          # 把 src/ 编译到 ui/
dbx-plugin package .   # 编译 sidecar 并生成 dist/*.dbxp
```

发布 GitHub Release 会触发 `.github/workflows/plugin-release.yml`，自动打包并交给 DBX 的公共发布流程。

## 日历数据

农历、24 节气、传统节日都是可计算的，任意年份都能得到，来自 [`chinese-days`](https://www.npmjs.com/package/chinese-days)。

法定节假日和调休由国务院每年公布，`chinese-days` 携带到 2026 年的安排。[`src/lunar.js`](src/lunar.js) 会探测当前渲染年份在该库里是否存在安排：有则每个格子额外显示 `休` / `班` 角标，没有则只显示农历与节气。

这里刻意不做任何兜底数据源。库里没有的年份就是没有节假日角标；下一年的安排通过升级依赖获得。

## 项目结构

```
src/
  App.svelte         月视图网格、日期格子、编辑框
  lunar.js           农历、节气、节假日优先级
  dbx.js             唯一接触宿主桥接的模块
  main.js            挂载应用
backend/
  main.go            sidecar 入口与 RPC 方法表
  notes.go           按命名空间原子存储备忘
  export.go          把导出文件写到用户指定的位置
  powershell.go      原生另存为、资源管理器定位与剪贴板（Windows）
  datadir.go         解析宿主提供的数据目录
assets/              插件图标
ui/                  构建产物 —— 自动生成，请勿手改
manifest.json        DBX 插件清单
dbx-plugin.toml      打包、后端与开发命令配置
```

## 技术栈

UI 用 Svelte 5（runes）+ Vite 7；sidecar 用 Go 1.22 与 DBX Go 插件 SDK。UI 只通过 `window.dbxPlugin` 桥接访问 sidecar。

清单字段、Host API、打包流程等详见 [DBX 插件开发文档](https://dbxio.com/en/docs/plugin-development)。

## 许可证

Apache-2.0，详见 [LICENSE](LICENSE)。
