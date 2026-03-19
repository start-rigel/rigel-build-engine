# AGENTS.md

## Core Rule

This repository must follow the shared project constraints defined in:

- `../rigel-core/AGENTS.md`

## Core Docs Location

Overall project documentation, workspace-level architecture, database notes, and deployment files are centralized in:

- `../rigel-core`

## Usage Rule

When working in this repository:

1. Read and follow `../rigel-core/AGENTS.md` first.
2. Treat `rigel-core` as the source of truth for workspace-level documentation.
3. Use this repository's local README and code layout only as module-specific supplements.
4. If a local module document conflicts with `rigel-core`, pause and reconcile instead of guessing.

## Security Supplement

1. `rigel-build-engine` 当前按内网服务设计；不要新增面向公网直接暴露的高成本推荐接口。
2. 对来自 `rigel-console` 的内部调用，默认要求服务级鉴权；如果新增内部入口，默认也要接入同一套 token 校验。
3. 触发真实 AI 的路径默认必须保留缓存、去重、并发闸门和超时控制，不要把高成本请求直接放到无保护路径上。
4. 不要在错误响应或日志中输出内部 token、上游 AI 密钥、原始敏感请求头。
