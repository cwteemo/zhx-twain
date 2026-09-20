@echo off
rem ============================================================
rem  scansvc maintenance tool (launcher)
rem
rem  Put this file and scansvc-tools.ps1 next to scansvc.exe,
rem  then double-click this file, or run it from a console:
rem
rem    scansvc-tools.bat                    menu
rem    scansvc-tools.bat status
rem    scansvc-tools.bat devices
rem    scansvc-tools.bat diagnose           TWAIN environment check
rem    scansvc-tools.bat dump "Uniscan Q400"
rem    scansvc-tools.bat options "Uniscan Q400"
rem    scansvc-tools.bat config
rem    scansvc-tools.bat builtin-config
rem    scansvc-tools.bat -Port 18081 status
rem
rem  All work is done by scansvc-tools.ps1 through Windows
rem  PowerShell, so curl.exe is NOT required.
rem ============================================================

setlocal

set "PS1=%~dp0scansvc-tools.ps1"
if not exist "%PS1%" (
    echo [ERROR] scansvc-tools.ps1 not found next to this file:
    echo         %PS1%
    echo         Copy both files together into the scansvc.exe folder.
    goto :end
)

where powershell.exe >nul 2>nul
if errorlevel 1 (
    echo [ERROR] powershell.exe not found. Windows 7 / Server 2008 R2 or newer is required.
    goto :end
)

powershell.exe -NoProfile -ExecutionPolicy Bypass -File "%PS1%" %*

:end
rem Keep the window open when started by double-click.
echo %cmdcmdline% | find /i "%~nx0" >nul
if not errorlevel 1 pause
endlocal
