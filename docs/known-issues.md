# 已知问题 / Known Issues

记录当前未修复、但已确认影响面与结论的问题，避免重复排查。

## 控制台 `preloadRoute` → `Cannot read properties of undefined (reading '_nonReactive')`

- **现象**:浏览器控制台出现 `TypeError: Cannot read properties of undefined (reading '_nonReactive')`,栈顶为 `preloadRoute`(`vendor-tanstack...`),异步经 `Promise.all` 触发。常在停留/切换页面(如「明细错误」页点「明细错误类型」标签)时偶发。
- **结论:非致命**。页面渲染与导航均正常,不影响任何功能;纯粹是后台**路由预取(preload)**阶段的报错。
- **根因**:`@tanstack/react-router`(当前 `^1.170.17`)在 `defaultPreload: 'intent'`(见 `web/default/src/main.tsx`)下,鼠标悬停 `<Link>`(主要是侧边栏导航)会触发 `preloadRoute`;该版本预取路由时访问到 `undefined` 路由的 `_nonReactive`,抛错。属库侧 preload bug。
- **与近期改动无关**:
  - 「明细错误类型 / 错误归因」分组视图**不渲染任何路由 `<Link>`**,也没有下钻按钮(下钻仅在「围绕明细错误查看」签名视图)。
  - 明细错误下钻「查看日志」用的是 `router.buildLocation(...)`(同步、不在报错栈里)+ `window.open` 开新标签页,不触发 preload。
  - 故此报错是悬停侧边栏链接触发的后台预取崩溃,与该标签页/下钻改动无关。
- **暂不修复**(2026-10-24 决定:先不动、记录在案)。可选修复:
  1. `web/default/src/main.tsx` 把 `defaultPreload: 'intent'` 改为 `false`(关掉悬停预取,立即消除报错,仅失去悬停预取提速,可逆)。
  2. 升级 `@tanstack/react-router` 到修掉该 preload bug 的补丁版本(更对症,但需 `bun install`、影响面更大)。
