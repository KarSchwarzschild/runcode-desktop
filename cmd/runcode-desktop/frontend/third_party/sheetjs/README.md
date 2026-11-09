# SheetJS CE 0.20.3

本目录保存官方原始 npm tarball，不修改包内源码。`package.json` 通过 `file:` 引用，
`package-lock.json` 固定 SHA512 integrity，避免每次安装/CI 都依赖境外 CDN。

- 来源：https://cdn.sheetjs.com/xlsx-0.20.3/xlsx-0.20.3.tgz
- 官方安装说明：https://docs.sheetjs.com/docs/getting-started/installation/nodejs/
- 下载日期：2026-10-19
- 包名 / 版本：`xlsx` / `0.20.3`
- 许可证：Apache-2.0；原包内 `package/LICENSE` 保持完整。
- SHA256：`8dc73fc3b00203e72d176e85b50938627c7b086e607c682e8d3c22c02bb99fe8`

公共 npm 仓库的 `xlsx` 仍停在 0.18.5。此次升级修复表格读取的
CVE-2023-30533（原型污染，0.19.3 修复）与 CVE-2024-22363（ReDoS，0.20.2 修复）。
哈希是本次从官方 HTTPS 地址取得的文件指纹，不是独立发布者签名。

升级时从官方来源取得明确版本，核对包名、版本和许可证，更新本文件及依赖声明，
由 npm 重生成锁文件，再运行表格解析测试、完整前端检查及 v3/v2 两条生产构建。
不要改回 npm 上的旧 `xlsx`，也不要只手改锁文件的版本/完整性字段。
