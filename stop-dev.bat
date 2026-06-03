@echo off
setlocal

cd /d "%~dp0"

echo Stopping assistant terminal windows...
taskkill /FI "WINDOWTITLE eq assistant-docker*" /T /F >nul 2>&1
taskkill /FI "WINDOWTITLE eq assistant-service*" /T /F >nul 2>&1
taskkill /FI "WINDOWTITLE eq assistant-web*" /T /F >nul 2>&1
taskkill /FI "WINDOWTITLE eq assistant-desktop*" /T /F >nul 2>&1

echo Stopping Electron processes...
taskkill /IM electron.exe /T /F >nul 2>&1

echo Stopping Vite/Node helper processes started from this repo...
powershell -NoProfile -Command ^
  "$repo = '%~dp0'.TrimEnd('\');" ^
  "$candidates = Get-CimInstance Win32_Process | Where-Object { " ^
  "($_.Name -match '^(node|cmd|powershell)(\.exe)?$') -and " ^
  "($_.CommandLine -like '*my-allrounder-agent*')" ^
  "};" ^
  "foreach ($proc in $candidates) { try { Stop-Process -Id $proc.ProcessId -Force -ErrorAction Stop } catch {} }"

echo Stopping Go service on port 8088...
powershell -NoProfile -Command ^
  "$conn = Get-NetTCPConnection -LocalAddress 127.0.0.1 -LocalPort 8088 -State Listen -ErrorAction SilentlyContinue;" ^
  "if ($conn) { try { Stop-Process -Id $conn.OwningProcess -Force -ErrorAction Stop } catch {} }"

echo Stopping Docker services...
docker-compose down

echo Done.

