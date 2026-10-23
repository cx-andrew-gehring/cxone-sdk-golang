# Setup your development environment

1. Ensure you have renamed the template folder to your project name.
2. Run the **setupdev** script (bat for Windows, sh for POSIX) from the project root.
   - If you have a GitHub repo, pass it as an argument:
     ```
     .devenv/setupdev.sh https://github.com/my-org/my-repo.git
     ```
   - This will automatically set everything up and push a new empty main branch + init branch.

#### The setup process will:
- Install Go dev and build requirements (golangci-lint, commitlint, goimports).
- Prepare basic configurations for VSCode (the .vscode folder).
- Create a pre-commit hook for source code analysis with golangci-lint.
- Create a commit-msg hook for enforcing commit conventions with CommitLint.
- Initialize a Go module with the Checkmarx One SDK as a dependency.

#### Commit message rules

Commit messages **MUST** comply with SemVer and conventional commits specifications, using Angular convention.

Accepted types:
- **chore**: major changes
- **feat**: new features
- **fix**: fixes
- **perf**: performance related
- **docs**: documentation related
- **build**: build process related
- **ci**: automation/pipeline related
- **refactor**: refactors not changing functionality
- **style**: presentation/UI related
- **test**: testing and QA related

#### References
- [Checkmarx One Go SDK](https://github.com/cx-andrew-gehring/cxone-sdk-golang)
- [GoLangCI Linter](https://github.com/golangci/golangci-lint/)
- [CommitLint](https://github.com/conventionalcommit/commitlint)
- [Conventional commits](https://www.conventionalcommits.org)
- [SemVer](https://semver.org)
