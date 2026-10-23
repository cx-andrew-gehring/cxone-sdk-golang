# Checkmarx One Go SDK — Project Template

Pre-configured project template for building Go tools with the [cxone-sdk-golang](https://github.com/cx-andrew-gehring/cxone-sdk-golang) SDK.

## How to use

1. Copy this `template/` directory and rename it to your project name:
   ```sh
   cp -r template/ my-cxone-tool
   cd my-cxone-tool
   ```

2. Optionally create an empty GitHub repository for your project.

3. Run the setup script:
   ```sh
   # Without a repo (set up git locally only):
   .devenv/setupdev.sh

   # With a repo (auto-push to GitHub):
   .devenv/setupdev.sh https://github.com/my-org/my-cxone-tool.git
   ```
   On Windows: `.devenv\setupdev.bat` (same arguments).

4. Set your environment variables and run:
   ```sh
   export CX_TENANT=your-tenant CX_API_KEY=your-key CX_REGION=US
   go run .
   ```

## What setup gives you

- **Git repo** initialized with `main` branch + `v0.0.0` tag
- **Go module** with the SDK wired in as a dependency
- **main.go** showing how to construct the SDK client, list projects, and browse the preset catalog
- **Code linting** via [golangci-lint](https://golangci-lint.run/) (pre-commit hook + CI)
- **Commit linting** via [commitlint](https://github.com/conventionalcommit/commitlint) (conventional commits enforced)
- **GitHub Actions** for PR validation (lint + test) and semantic-release with auto-changelog
- **Goreleaser** config for building Linux/Windows/macOS binaries
- **VSCode** workspace settings and debug launch config

## SDK documentation

- [SDK README](https://github.com/cx-andrew-gehring/cxone-sdk-golang/blob/main/README.md)
- [SDK examples](https://github.com/cx-andrew-gehring/cxone-sdk-golang/tree/main/examples)
- [CLAUDE.md architectural contract](https://github.com/cx-andrew-gehring/cxone-sdk-golang/blob/main/CLAUDE.md)
