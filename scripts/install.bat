@echo off
setlocal EnableDelayedExpansion
rem dbx Windows 安装脚本：把与脚本同目录的 dbx.exe 拷贝到安装目录并加入用户 PATH
rem 用法（双击或 cmd 执行均可，无执行策略限制）：
rem   install.bat              安装到默认目录 %%LOCALAPPDATA%%\dbx
rem   install.bat D:\dbx   安装到自定义目录
rem 安装完成后新开一个终端即可直接使用 dbx 命令

set "SRC=%~dp0dbx.exe"
set "DEST_DIR=%~1"
if "%DEST_DIR%"=="" set "DEST_DIR=%LOCALAPPDATA%\dbx"
set "DEST=%DEST_DIR%\dbx.exe"

if not exist "%SRC%" (
  echo 错误: 未在脚本同目录找到 dbx.exe (%~dp0)
  exit /b 1
)

if not exist "%DEST_DIR%" mkdir "%DEST_DIR%" 2>nul || (echo 错误: 无法创建目录 %DEST_DIR%& exit /b 1)
copy /y "%SRC%" "%DEST%" >nul || (echo 错误: 拷贝失败& exit /b 1)

rem 执行校验：平台/文件损坏等问题在此暴露，避免误报安装成功
"%DEST%" version >nul 2>&1 || (echo 错误: 安装的二进制无法执行，请确认 zip 平台与本机架构匹配& exit /b 1)

rem 读取当前用户 PATH（直接查注册表，避免 setx 的 1024 字符截断问题）
set "UPATH="
for /f "tokens=2,*" %%a in ('reg query "HKCU\Environment" /v Path 2^>nul ^| findstr /i "Path"') do set "UPATH=%%b"

rem 已包含安装目录则跳过，否则追加（保持 REG_EXPAND_SZ 类型，保留 %%VAR%% 展开）
echo ;!UPATH!; | find /i ";%DEST_DIR%;" >nul
if errorlevel 1 (
  if defined UPATH (
    reg add "HKCU\Environment" /v Path /t REG_EXPAND_SZ /d "!UPATH!;%DEST_DIR%" /f >nul || (echo 错误: 写入 PATH 失败& exit /b 1)
  ) else (
    reg add "HKCU\Environment" /v Path /t REG_EXPAND_SZ /d "%DEST_DIR%" /f >nul || (echo 错误: 写入 PATH 失败& exit /b 1)
  )
  echo 已加入用户 PATH: %DEST_DIR%（新开终端生效）
) else (
  echo 已在 PATH 中: %DEST_DIR%
)

for /f "delims=" %%v in ('"%DEST%" version') do echo 安装完成: %%v -^> %DEST%
endlocal
