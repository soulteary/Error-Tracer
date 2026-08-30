# 发布流程

[English](releasing.md)

只有维护者可以创建发布标签。发布 Workflow 会产生外部制品，普通 PR 审查本身不会
触发发布。

## 打标签之前

1. 合并本次发布需要的全部变更，并确认 `main` 的 CI 全部通过。
2. 确认 `VERSION`、`package.json` 和变更日志使用相同的
   `MAJOR.MINOR.PATCH` 版本；同时把该版本标题中的 `Unreleased` 替换为 UTC
   发布日期（`YYYY-MM-DD`）。
3. 执行普通测试、竞态检测、浏览器测试、漏洞扫描和文档检查。
4. 在 Linux 上将完全相同的归档构建到一个新路径：

   ```sh
   scripts/build-release.sh 2.0.0 /tmp/error-tracer-release
   ```

5. 创建标签前，使用 Linux 归档实际运行 `version` 和 `demo`。

## 创建发布

从已审查的 `main` 提交创建带签名的附注标签，并且只推送该标签：

```sh
git tag -s v2.0.0 -m "Error-Tracer 2.0.0"
git push origin v2.0.0
```

标签 Workflow 会拒绝非稳定 SemVer 标签和版本不一致。随后它会：

- 重新执行 Go 测试、竞态检测和浏览器测试；
- 为 Linux、macOS、Windows 的 AMD64/ARM64 创建可复现归档；
- 生成 SHA-256 校验和及 SPDX JSON SBOM；
- 创建带签名的 GitHub 来源证明；
- 准备草稿 GitHub Release；
- 构建本地候选镜像并执行 Demo 冒烟测试；
- 发布并验证带 BuildKit 来源证明和 SBOM 的精确版本
  `linux/amd64`、`linux/arm64` 镜像；
- 验证成功后再提升主版本、次版本及 `latest` 镜像别名；
- 所有检查成功后才公开 GitHub Release。

如果草稿创建后的步骤失败，Release 会保持私有草稿状态，可重新运行 Workflow。
已经公开的版本标签不得移动；应改为准备补丁版本。

## 验证发布制品

```sh
sha256sum --check checksums.txt
gh attestation verify error-tracer_2.0.0_linux_amd64.tar.gz \
  --repo soulteary/Error-Tracer
docker run --rm --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges:true \
  -p 127.0.0.1:8080:8080 ghcr.io/soulteary/error-tracer:2 demo
```

打开 <http://127.0.0.1:8080/>，确认无需凭据和数据库即可加载样例工作区。
