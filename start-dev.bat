@echo off
setlocal

cd /d "%~dp0"

set "ROOT=%~dp0"
set "GO_EXE=%ROOT%.tools\go\bin\go.exe"
set "APP_AUTH_TOKEN=local-dev-token"

echo [0/6] Preparing development environment...

echo [1/6] Starting Docker dependencies...
docker-compose up -d
if errorlevel 1 (
  echo Docker dependencies did not start cleanly. Continuing in degraded-friendly mode.
) else (
  call :wait_for_port "MongoDB" 127.0.0.1 27017 12
  call :wait_for_port "Neo4j" 127.0.0.1 7687 12
)

echo [2/6] Starting Go service...
start "assistant-service" cmd /k "cd /d "%ROOT%service" && set "APP_AUTH_TOKEN=%APP_AUTH_TOKEN%" && "%GO_EXE%" run ./cmd/service"
call :wait_for_http "Go service" "http://127.0.0.1:8088/health" 20

echo [3/6] Starting web UI...
start "assistant-web" cmd /k "cd /d "%ROOT%" && set "VITE_API_BASE_URL=http://127.0.0.1:8088" && set "VITE_APP_AUTH_TOKEN=%APP_AUTH_TOKEN%" && npm run dev:web"
call :wait_for_http "Web UI" "http://localhost:5173/" 30

echo [4/6] Starting Electron desktop...
start "assistant-desktop" cmd /k "cd /d "%ROOT%" && set "APP_AUTH_TOKEN=%APP_AUTH_TOKEN%" && set "APP_MANAGED_SERVICE=1" && npm run dev:desktop"

echo [5/6] Quick health check...
powershell -NoProfile -Command "try { (Invoke-WebRequest -UseBasicParsing http://127.0.0.1:8088/status -Method GET).Content } catch { $_.Exception.Message }"

echo [6/6] Done. Use stop-dev.bat to shut everything down cleanly.
goto :eof

:wait_for_port
set "WAIT_NAME=%~1"
set "WAIT_HOST=%~2"
set "WAIT_PORT=%~3"
set "WAIT_ATTEMPTS=%~4"
set /a WAIT_COUNT=0
:wait_for_port_loop
set /a WAIT_COUNT+=1
powershell -NoProfile -Command "$client = New-Object Net.Sockets.TcpClient; try { $client.Connect('%WAIT_HOST%', %WAIT_PORT%); exit 0 } catch { exit 1 } finally { $client.Dispose() }"
if not errorlevel 1 (
  echo %WAIT_NAME% is ready on %WAIT_HOST%:%WAIT_PORT%.
  goto :eof
)
if %WAIT_COUNT% geq %WAIT_ATTEMPTS% (
  echo %WAIT_NAME% did not become ready in time. Continuing.
  goto :eof
)
timeout /t 1 /nobreak >nul
goto :wait_for_port_loop

:wait_for_http
set "WAIT_NAME=%~1"
set "WAIT_URL=%~2"
set "WAIT_ATTEMPTS=%~3"
set /a WAIT_COUNT=0
:wait_for_http_loop
set /a WAIT_COUNT+=1
powershell -NoProfile -Command "try { $response = Invoke-WebRequest -UseBasicParsing '%WAIT_URL%' -Method GET; if ($response.StatusCode -eq 200) { exit 0 } else { exit 1 } } catch { exit 1 }"
if not errorlevel 1 (
  echo %WAIT_NAME% is reachable at %WAIT_URL%.
  goto :eof
)
if %WAIT_COUNT% geq %WAIT_ATTEMPTS% (
  echo %WAIT_NAME% did not become reachable in time. Continuing.
  goto :eof
)
timeout /t 1 /nobreak >nul
goto :wait_for_http_loop
