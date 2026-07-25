# my-utils-api documentation

## Current documents

| Document | Scope | Authority |
| --- | --- | --- |
| [`../AGENTS.md`](../AGENTS.md) | development workflow and invariants | primary instructions |
| [`../README.md`](../README.md) | setup, commands, configuration and deployment | project overview |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | runtime architecture and access model | architecture reference |
| [`UTILS-WORKSPACE.md`](UTILS-WORKSPACE.md) | frontend/backend contract | integration reference |

Historical implementation plans and specifications live under `superpowers/`.
They provide decision history but do not override current code, CI
configuration, or the documents above.

## Source-of-truth order

When references disagree:

1. current executable code, migrations, compose files, and `.woodpecker.yml`;
2. repository `AGENTS.md`;
3. project README and `ARCHITECTURE.md`;
4. integration/reference documents;
5. historical plans and specifications.

Update the closest authoritative document whenever code or deployment
configuration changes its behavior.
