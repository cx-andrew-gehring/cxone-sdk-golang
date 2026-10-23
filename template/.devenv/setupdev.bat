@ECHO OFF
SETLOCAL EnableDelayedExpansion

REM --------------------------
REM SETUP YOUR DEV ENVIRONMENT
REM --------------------------
REM This shall be the first thing to run on your development environment
REM --------------------------

REM Get current directory name
FOR %%i IN ("%CD%") DO SET "current_dir_name=%%~nxi"

REM Check if running from a template clone
REM Using /I for case-insensitive comparison
IF /I "%current_dir_name%"=="cxone-golang-template" (
    ECHO You are running this from a clone of the template repository %current_dir_name% - Please rename the folder to your project name.
    EXIT /B 1
)

REM Check if running from the .devenv folder
IF "%current_dir_name%"==".devenv" (
    ECHO You are running this from the .devenv folder - please run this script from the project root folder.
    EXIT /B 1
)

REM Navigate to the script's directory and then to the project root
CD /D "%~dp0"
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
CD ..
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

ECHO Setting up new local git repo
RMDIR /S /Q .git >NUL 2>&1
git init --initial-branch=main
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

DEL /F /Q README.md >NUL 2>&1
COPY /Y .devenv\readme.template README.md
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

DEL /F /Q .gitignore >NUL 2>&1
COPY /Y .devenv\.gitignore.template .gitignore
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

REM Copy .github directory recursively
XCOPY .devenv\.github .github /E /I /Y >NUL 2>&1
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

SET "template=.devenv\.goreleaser.yaml.template"
SET "target=.goreleaser.yaml"

REM Get the repository URL from the first argument, removing quotes
SET "repoUrl=%~1"

SET "gitowner=cx-andrew-gehring"
SET "gitrepo=%current_dir_name%"

IF NOT "%repoUrl%"=="" (
    REM Remove "https://github.com/" prefix
    SET "cleanUrl=%repoUrl:https://github.com/=%"
    REM Remove ".git" suffix
    SET "cleanUrl=%cleanUrl:.git=%"

    REM Extract gitowner (first part before '/')
    FOR /F "tokens=1 delims=/" %%A IN ("%cleanUrl%") DO SET "gitowner=%%A"
    REM Extract gitrepo (second part after '/')
    FOR /F "tokens=2 delims=/" %%A IN ("%cleanUrl%") DO SET "gitrepo=%%A"
)

ECHO Git owner: %gitowner%

ECHO Creating %target%...
(
    ECHO version: 2
    ECHO project_name: %gitrepo%
    ECHO release:
    ECHO   github:
    ECHO     owner: %gitowner%
) > "%target%"
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

TYPE "%template%" >> "%target%"
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

git add .github .goreleaser.yaml .releaserc.json .gitattributes .gitignore .golangci.yml README.md
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
git commit -m "chore(init): Empty repo [skip ci]"
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
git tag v0.0.0
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
git checkout -b init
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

ECHO Installing go commit lint
go install github.com/conventionalcommit/commitlint@latest >NUL 2>&1
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

ECHO Installing golangci-lint
go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@latest >NUL 2>&1
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

ECHO Installing goimports
go install golang.org/x/tools/cmd/goimports@latest >NUL 2>&1
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

ECHO Copying .vscode files (if needed)
IF NOT EXIST ".vscode" MD ".vscode"
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

IF NOT EXIST ".vscode\launch.json" COPY /Y .devenv\.settings\.vscode-launch.json .vscode\launch.json
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

IF NOT EXIST ".vscode\settings.json" COPY /Y .devenv\.settings\.vscode-settings.json .vscode\settings.json
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

ECHO Copying git hooks
COPY /Y .devenv\.settings\.commit-msg-hook .git\hooks\commit-msg
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
COPY /Y .devenv\.settings\.pre-commit-hook .git\hooks\pre-commit
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
COPY /Y .devenv\.settings\.checkcode.bat checkcode.bat
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
COPY /Y .devenv\.settings\.checkcode.sh checkcode.sh
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

IF EXIST "go.mod" DEL /F /Q go.mod >NUL 2>&1

ECHO Initializing new go module
go mod init "github.com/%gitowner%/%gitrepo%" >NUL 2>&1
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
go mod tidy >NUL 2>&1
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
git add .
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
git commit -m "fix(init): set up initial bare template"
IF %ERRORLEVEL% NEQ 0 GOTO :error_exit

IF "%repoUrl%"=="" (
    ECHO No parameter was provided to the setupdev.bat script
    ECHO Once you set up a repo somewhere like github.com you will need to manually run the following
    ECHO     git remote add origin https://github.com/your-org/your-repo.git
    ECHO     git push -u origin main
    ECHO     git push -u origin v0.0.0
    ECHO     git push -u origin init
    ECHO You will also need to edit the '.goreleaser.yaml' file to update the project name and github owner organization at the top.
) ELSE (
    git remote add origin "%repoUrl%"
    IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
    git push -u origin main
    IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
    git push origin v0.0.0
    IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
    git push -u origin init
    IF %ERRORLEVEL% NEQ 0 GOTO :error_exit
)

ECHO Development environment setup complete!
GOTO :eof

:error_exit
ECHO An error occurred: %ERRORLEVEL%. Exiting.
ENDLOCAL
EXIT /B %ERRORLEVEL%
