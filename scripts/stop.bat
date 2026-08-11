@echo off
rem dbx 停止脚本：终止正在运行的 dbx Web 服务
rem 用法：双击运行或 cmd 执行 stop.bat

for /f "tokens=2" %%a in ('tasklist /fi "imagename eq dbx.exe" /fo csv /nh 2^>nul ^| findstr /i "dbx.exe"') do (
  set "PID=%%~a"
  goto :found
)
echo 未找到正在运行的 dbx 进程
pause
exit /b 0

:found
echo 终止 dbx 进程 (PID: %PID%)...
taskkill /pid %PID% /f >nul 2>&1
echo dbx 已停止
pause
