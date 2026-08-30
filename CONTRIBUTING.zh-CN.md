# 参与 Error-Tracer 开发

[English](CONTRIBUTING.md)

感谢你帮助改进 Error-Tracer。每个 PR 应尽量只解决一个便于审查的问题，并为行为
变化补充测试。

## 开发环境

使用 `go.mod` 声明的 Go 版本。只有浏览器 SDK 测试需要 Node.js 22 或更高版本。

```sh
go mod verify
go vet ./...
go test ./...
go test -race ./...
npm test
```

基准和 HTTP 压力测试必须使用[性能指南](docs/performance.md)中的有界命令。未获得
明确授权时，不得对任何系统发送压力测试流量。

## Pull Request

- 使用 `gofmt` 格式化 Go 文件；除非依赖具有明确运维收益，否则保持 JavaScript
  零依赖。
- 修改采集或存储代码时，继续保证原子性、项目隔离、输入边界和凭据安全。
- 公共命令、选项、接口或 Dashboard 文案变化时，同时更新英文和简体中文文档。
- 不提交 SQLite 数据库、凭据、生成的二进制、覆盖率文件或压力测试输出。
- 在 PR 中说明兼容性变化，并列出实际执行的验证命令。

安全问题请按[安全策略](SECURITY.md)私下报告，不要在公开 Issue 中披露利用细节。
