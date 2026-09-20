<img src="assets/icon.svg" width="96" alt="日历清单图标">

# 日历清单

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
- 内容保存在 `localStorage`

**与宿主集成**

- 跟随 DBX 宿主的语言设置，中文或英文
- 使用宿主的 `Canvas` / `CanvasText` 主题色

## 环境要求

- DBX `>=0.5.68`，Host API `1`
- 开发需要 Node.js 与 `dbx-plugin` CLI

## 开发

```bash
npm install
dbx-plugin dev --path . --port 5190
```

`src/` 下的源码由 Vite 编译到 `ui/`，宿主加载的是 `ui/`。最终集成测试请用真实的 DBX 宿主。

## 构建与发布

```bash
npm run build          # 把 src/ 编译到 ui/
dbx-plugin package .   # 生成 dist/*.dbxp
```

发布 GitHub Release 会触发 `.github/workflows/plugin-release.yml`，自动打包并交给 DBX 的公共发布流程。

## 日历数据

农历、24 节气、传统节日都是可计算的，任意年份都能得到，来自 [`chinese-days`](https://www.npmjs.com/package/chinese-days)。

法定节假日和调休由国务院每年公布，有两个来源，由 [`src/lunar.js`](src/lunar.js) 选择：

1. `chinese-days` 包，携带到 2026 年的安排。
2. `json/holiday-<年>.json`，由 [`src/holiday-data.js`](src/holiday-data.js) 打包，用于内置包未覆盖的年份。

两个来源都没有该年数据时，日历只显示农历与节气，不带 `休` / `班` 角标。

### 更新节假日数据

官方公布安排后，新增 `json/holiday-<年>.json` 再重新构建即可。无需改代码，也无需升级依赖。

```jsonc
{
  "holiday": {
    "01-01": { "holiday": true,  "name": "元旦" },
    "01-04": { "holiday": false, "name": "元旦后补班", "target": "元旦" }
  }
}
```

`holiday: true` 表示放假，`holiday: false` 表示补班。代码只读取 `name` 和 `holiday` 两个字段；仓库里的文件还带 `date`、`wage`、`rest` 等字段，插件不使用。

这些文件在构建期内联进包体。打包后的插件是自包含的，运行时不会读写磁盘。

## 项目结构

```
src/
  App.svelte         月视图网格、日期格子、编辑框
  lunar.js           农历、节气、节假日优先级
  holiday-data.js    内联 json/holiday-<年>.json
  main.js            挂载应用
json/                节假日安排，每年一个文件
assets/              插件图标
ui/                  构建产物 —— 自动生成，请勿手改
manifest.json        DBX 插件清单
dbx-plugin.toml      打包与开发命令配置
```

## 技术栈

Svelte 5（runes）+ Vite 7。UI 通过 `window.dbxPlugin` 桥接与宿主通信。

清单字段、Host API、打包流程等详见 [DBX 插件开发文档](https://dbxio.com/en/docs/plugin-development)。

## 许可证

Apache-2.0，详见 [LICENSE](LICENSE)。
