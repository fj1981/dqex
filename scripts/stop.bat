@echo off
rem dqex 停止脚本：终止正在运行的 dqex Web 服务
rem 用法：双击运行或 cmd 执行 stop.bat

for /f "tokens=2" %%a in ('tasklist /fi "imagename eq dqex.exe" /fo csv /nh 2^>nul ^| findstr /i "dqex.exe"') do (
  set "PID=%%~a"
  goto :found
)
echo 未找到正在运行的 dqex 进程
pause
exit /b 0

:found
echo 终止 dqex 进程 (PID: %PID%)...
taskkill /pid %PID% /f >nul 2>&1
echo dqex 已停止
pause
