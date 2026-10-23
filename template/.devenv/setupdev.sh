#!/bin/bash
# --------------------------
# SETUP YOUR DEV ENVIRONMENT
# --------------------------
# This shall be the first thing to run on your development environment
# --------------------------

set -e

current_dir_name=$(basename "$PWD")

if [[ "${current_dir_name,,}" == "cxone-golang-template" ]]; then
    echo "You are running this from a clone of the template repository $current_dir_name - Please rename the folder to your project name."
    exit 1
fi

if [[ "$current_dir_name" == ".devenv" ]]; then
    echo "You are running this from the .devenv folder - please run this script from the project root folder."
    exit 1
fi

# Navigate to the script's directory and then to the project root
cd "$(dirname "$0")"
cd ..

# We are in a new project location, but may have the template's .git folder
echo "Setting up new local git repo"
rm -rf .git > /dev/null 2>&1
git init --initial-branch=main

rm -f README.md
cp .devenv/readme.template README.md
rm -f .gitignore
cp .devenv/.gitignore.template .gitignore
cp -r .devenv/.github .

template=".devenv/.goreleaser.yaml.template"
target=".goreleaser.yaml"

repoUrl="$1"

if [ -z "$repoUrl" ]; then
    gitowner="cx-andrew-gehring"
    gitrepo="$current_dir_name"
else
    cleanUrl="${repoUrl#https://github.com/}"
    cleanUrl="${cleanUrl%.git}"
    gitowner=$(echo "$cleanUrl" | cut -d'/' -f1)
    gitrepo=$(echo "$cleanUrl" | cut -d'/' -f2)
fi

echo "Git owner: $gitowner"

echo "Creating $target..."
cat << EOF > "$target"
version: 2
project_name: $gitrepo
release:
  github:
    owner: $gitowner
EOF
cat "$template" >> "$target"

git add .github .goreleaser.yaml .releaserc.json .gitattributes .gitignore .golangci.yml README.md
git commit -m "chore(init): Empty repo [skip ci]"
git tag v0.0.0
git checkout -b init

echo "Installing go commit lint"
go install github.com/conventionalcommit/commitlint@latest > /dev/null 2>&1

echo "Installing go linters"
curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/HEAD/install.sh | sh -s -- -b $(go env GOPATH)/bin > /dev/null 2>&1

echo "Installing goimports"
go install golang.org/x/tools/cmd/goimports@latest > /dev/null 2>&1

echo "Copying .vscode files (if needed)"
if [ ! -d ".vscode" ]; then
    mkdir .vscode
fi
if [ ! -f ".vscode/launch.json" ]; then
    cp .devenv/.settings/.vscode-launch.json .vscode/launch.json
fi
if [ ! -f ".vscode/settings.json" ]; then
    cp .devenv/.settings/.vscode-settings.json .vscode/settings.json
fi

echo "Copying git hooks"
cp .devenv/.settings/.commit-msg-hook .git/hooks/commit-msg
cp .devenv/.settings/.pre-commit-hook .git/hooks/pre-commit
cp .devenv/.settings/.checkcode.bat checkcode.bat
cp .devenv/.settings/.checkcode.sh checkcode.sh
chmod +x .git/hooks/commit-msg .git/hooks/pre-commit checkcode.sh

if [ -f "go.mod" ]; then
    rm go.mod
fi

echo "Initializing new go module"
go mod init "github.com/$gitowner/$gitrepo" > /dev/null 2>&1
go mod tidy > /dev/null 2>&1
git add .
git commit -m "fix(init): set up initial bare template"

if [ -z "$1" ]; then
    echo "No parameter was provided to the setupdev.sh script"
    echo "Once you set up a repo somewhere like github.com you will need to manually run the following"
    echo "    git remote add origin https://github.com/your-org/your-repo.git"
    echo "    git push -u origin main"
    echo "    git push -u origin v0.0.0"
    echo "    git push -u origin init"
    echo "You will also need to edit the '.goreleaser.yaml' file to update the project name and github owner organization at the top."
else
    git remote add origin "$1"
    git push -u origin main
    git push origin v0.0.0
    git push -u origin init
fi

echo "Development environment setup complete!"
