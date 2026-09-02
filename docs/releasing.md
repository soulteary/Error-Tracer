# Release process

[简体中文](releasing.zh-CN.md)

Only maintainers should create a release tag. The release workflow publishes
artifacts, so reviewing a pull request does not create a release by itself.

## Before tagging

1. Merge every change intended for the release and require a green CI result on
   `main`.
2. Confirm that `VERSION`, `package.json`, and the changelog name the same
   `MAJOR.MINOR.PATCH` version. Replace `Unreleased` in that changelog heading
   with the UTC release date (`YYYY-MM-DD`).
3. Run the normal test suite, race detector, browser tests, vulnerability scan,
   and documentation checks.
4. Build the exact archives locally on Linux into a new path:

   ```sh
   go install github.com/soulteary/ci-recipes/cmd/ci-recipes@6e790adf553ecff9f5ba5a3d0beeb9a9256a29ee
   ci-recipes error-tracer build-release 2.0.0 /tmp/error-tracer-release
   ```

5. Exercise the Linux archive with `version` and `demo` before creating the
   tag.

## Create the release

Create a signed, annotated tag from the reviewed `main` commit and push only
that tag:

```sh
git tag -s v2.0.0 -m "Error-Tracer 2.0.0"
git push origin v2.0.0
```

The tag workflow rejects non-stable SemVer tags or a version mismatch. It then:

- reruns Go, race, and browser tests;
- creates deterministic Linux, macOS, and Windows archives for AMD64 and ARM64;
- generates SHA-256 checksums and an SPDX JSON SBOM;
- creates signed GitHub provenance attestations;
- prepares a draft GitHub release;
- builds and smoke-tests a local release image;
- publishes and verifies the exact `linux/amd64` and `linux/arm64` version image
  with BuildKit provenance and SBOM attestations;
- promotes the verified image to its major, minor, and `latest` aliases; and
- makes the GitHub release public only after those checks pass.

If a step after draft creation fails, the release stays private as a draft and
the workflow can be rerun. Never move an already published version tag; prepare
a patch release instead.

## Verify published artifacts

```sh
sha256sum --check checksums.txt
gh attestation verify error-tracer_2.0.0_linux_amd64.tar.gz \
  --repo soulteary/Error-Tracer
docker run --rm --read-only --cap-drop=ALL \
  --security-opt=no-new-privileges:true \
  -p 127.0.0.1:8080:8080 ghcr.io/soulteary/error-tracer:2 demo
```

Open <http://127.0.0.1:8080/> and confirm that the sample workspace loads
without credentials or a database.
