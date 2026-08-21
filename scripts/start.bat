@echo off
setlocal EnableDelayedExpansion
rem dqex 启动脚本：检查安装状态并启动 Web 服务
rem 用法（双击或 cmd 执行均可）：
rem   start.bat                        前台运行（关闭窗口即停止）
rem   start.bat -d                     后台运行（关闭窗口不中断）
rem   start.bat -d --port 9000         后台 + 指定端口

set "DIR=%~dp0"
set "DBX="

rem 优先同目录 > PATH
if exist "%DIR%dqex.exe" (
  set "DBX=%DIR%dqex.exe"
) else (
  where dqex >nul 2>&1
  if !errorlevel! equ 0 (
    set "DBX=dqex"
  )
)

if "%DBX%"=="" (
  echo 错误: 未找到 dqex，请先执行 install.bat 安装
  pause
  exit /b 1
)

rem 解析参数，提取 -d
set "DAEMON=0"
set "ARGS="
:parse
if "%~1"=="" goto :run
if /i "%~1"=="-d" set "DAEMON=1" & shift & goto :parse
if /i "%~1"=="--daemon" set "DAEMON=1" & shift & goto :parse
set "ARGS=!ARGS! %~1"
shift
goto :parse

:run
echo 使用: %DBX%

if "%DAEMON%"=="1" (
  start /b "" "%DBX%" %ARGS% >nul 2>&1
  echo dqex 已在后台启动
  echo 查看访问链接: dqex url
) else (
  echo 启动 Web 服务（关闭窗口即停止）...
  "%DBX%" %ARGS%
  if !errorlevel! neq 0 pause
)
endlocal
