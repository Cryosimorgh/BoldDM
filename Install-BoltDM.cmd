@echo off
setlocal
set "SRC=%~dp0"
set "DEST=%LOCALAPPDATA%\Programs\BoltDM"

echo Installing BoltDM to:
echo   %DEST%
echo.
if not exist "%DEST%" mkdir "%DEST%"

powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Get-ChildItem -LiteralPath '%SRC%' -File -ErrorAction SilentlyContinue ^| Unblock-File -ErrorAction SilentlyContinue"
copy /Y "%SRC%BoltDM.exe" "%DEST%\BoltDM.exe" >nul || goto :fail
if exist "%SRC%BoltDM.ico" copy /Y "%SRC%BoltDM.ico" "%DEST%\BoltDM.ico" >nul || goto :fail

powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "$ws=New-Object -ComObject WScript.Shell; $p=Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\BoltDM.lnk'; $s=$ws.CreateShortcut($p); $s.TargetPath=Join-Path $env:LOCALAPPDATA 'Programs\BoltDM\BoltDM.exe'; $s.WorkingDirectory=Join-Path $env:LOCALAPPDATA 'Programs\BoltDM'; $ico=Join-Path $env:LOCALAPPDATA 'Programs\BoltDM\BoltDM.ico'; if (Test-Path $ico) { $s.IconLocation=$ico }; $s.Save()"

powershell.exe -NoProfile -ExecutionPolicy Bypass -Command "Get-ChildItem -LiteralPath '%DEST%' -File ^| Unblock-File -ErrorAction SilentlyContinue"
start "" "%DEST%\BoltDM.exe"
echo BoltDM installed.
exit /b 0

:fail
echo Installation failed. If BoltDM is currently running, exit it from the tray and run this installer again.
exit /b 1
