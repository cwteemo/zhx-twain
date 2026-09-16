@echo off
rem ============================================================
rem  scansvc build script
rem
rem    build.bat                 build DLL + service, package into dist\
rem    build.bat dll             only the TWAIN DLL (Visual Studio)
rem    build.bat go              only scansvc.exe (Go)
rem    build.bat -debug          Debug build of the DLL (default: Release)
rem    build.bat -console        keep the console window (default: no window, tray only)
rem    build.bat -out D:\deploy  package into another folder (default: dist\)
rem    build.bat -notest         skip Go unit tests
rem
rem  Needs: Visual Studio 2017+ (C++), Go 1.19+, 64-bit MinGW-w64 gcc (for cgo).
rem  Everything the service needs at runtime is copied into the output folder.
rem ============================================================

setlocal EnableDelayedExpansion

set "ROOT=%~dp0"
set "SLN=%ROOT%TWAIN-Samples\Twain_App_sample01\visual_studio\TWAIN_APP_VS2017.sln"
set "DLLOUT=%ROOT%TWAIN-Samples\Twain_App_sample01\visual_studio"
set "SVC=%ROOT%TWAIN-Samples\Twain_App_sample01\src\scansvc"
set "FREEIMAGE=%ROOT%TWAIN-Samples\pub\external\bin\win64\FreeImage.dll"

set "CONFIG=Release"
set "LDFLAGS=-H=windowsgui"
set "OUT=%ROOT%dist"
set "DO_DLL=1"
set "DO_GO=1"
set "DO_TEST=1"

:parse
if "%~1"=="" goto parsed
if /i "%~1"=="dll"       ( set "DO_GO=0"  & shift & goto parse )
if /i "%~1"=="go"        ( set "DO_DLL=0" & shift & goto parse )
if /i "%~1"=="all"       ( shift & goto parse )
if /i "%~1"=="-debug"    ( set "CONFIG=Debug" & shift & goto parse )
if /i "%~1"=="-console"  ( set "LDFLAGS=" & shift & goto parse )
if /i "%~1"=="-notest"   ( set "DO_TEST=0" & shift & goto parse )
if /i "%~1"=="-out"      ( set "OUT=%~2" & shift & shift & goto parse )
echo [ERROR] unknown argument: %~1
echo.
echo   usage: build.bat [all^|dll^|go] [-debug] [-console] [-notest] [-out ^<dir^>]
goto fail
:parsed

echo ============================================================
echo  config : %CONFIG% ^| x64
if defined LDFLAGS (echo  exe    : no console window ^(tray only^)) else (echo  exe    : with console window)
echo  output : %OUT%
echo ============================================================
echo.

rem ---------------- 1. TWAIN DLL ----------------
if "%DO_DLL%"=="0" goto skipdll

set "MSBUILD="
set "VSWHERE=%ProgramFiles(x86)%\Microsoft Visual Studio\Installer\vswhere.exe"
if exist "%VSWHERE%" (
    for /f "usebackq tokens=*" %%i in (`"%VSWHERE%" -latest -products * -requires Microsoft.Component.MSBuild -find MSBuild\**\Bin\MSBuild.exe`) do set "MSBUILD=%%i"
)
if not defined MSBUILD (
    where msbuild.exe >nul 2>nul && set "MSBUILD=msbuild.exe"
)
if not defined MSBUILD (
    echo [ERROR] MSBuild not found. Install Visual Studio with the C++ workload,
    echo         or run this script from a "Developer Command Prompt for VS".
    goto fail
)

echo [1/3] building DLL ...
"%MSBUILD%" "%SLN%" /t:Build /p:Configuration=%CONFIG% /p:Platform=x64 /m /nologo /v:minimal
if errorlevel 1 (
    echo [ERROR] DLL build failed. See the messages above.
    goto fail
)

if not exist "%DLLOUT%\%CONFIG%\TWAIN_APP_CMD64.dll" (
    echo [ERROR] built, but TWAIN_APP_CMD64.dll is missing in %DLLOUT%\%CONFIG%
    goto fail
)
rem cgo links against the .lib and loads the .dll, both must sit next to the Go sources
copy /y "%DLLOUT%\%CONFIG%\TWAIN_APP_CMD64.dll" "%SVC%\" >nul
copy /y "%DLLOUT%\%CONFIG%\TWAIN_APP_CMD64.lib" "%SVC%\" >nul
echo       TWAIN_APP_CMD64.dll/.lib -^> src\scansvc\
echo.
:skipdll

rem ---------------- 2. scansvc.exe ----------------
if "%DO_GO%"=="0" goto skipgo

where go.exe >nul 2>nul
if errorlevel 1 (
    echo [ERROR] go.exe not found in PATH. Install Go 1.19+.
    goto fail
)
where gcc.exe >nul 2>nul
if errorlevel 1 (
    echo [ERROR] gcc.exe not found in PATH. cgo needs 64-bit MinGW-w64.
    goto fail
)
rem 32-bit MinGW is the classic trap: it compiles but cannot link the 64-bit DLL
set "GCCARCH="
for /f "usebackq tokens=*" %%i in (`gcc -dumpmachine`) do set "GCCARCH=%%i"
echo !GCCARCH! | findstr /i "x86_64" >nul
if errorlevel 1 (
    echo [ERROR] gcc is "!GCCARCH!", not 64-bit. Install MinGW-w64 x86_64 and put
    echo         its bin folder first in PATH. Expected: x86_64-w64-mingw32
    goto fail
)
if not exist "%SVC%\TWAIN_APP_CMD64.lib" (
    echo [ERROR] %SVC%\TWAIN_APP_CMD64.lib is missing.
    echo         Build the DLL first:  build.bat dll
    goto fail
)

pushd "%SVC%"
set CGO_ENABLED=1
set GOARCH=amd64
set GOOS=windows

if "%DO_TEST%"=="1" (
    echo [2/3] running tests ...
    rem only the pure-Go packages: the main package needs the DLL and cannot be tested here
    go test ./imgfmt ./scanopt
    if errorlevel 1 (
        echo [ERROR] tests failed. Build stopped.
        popd
        goto fail
    )
    echo.
)

echo [3/3] building scansvc.exe ...
if defined LDFLAGS (
    go build -ldflags "%LDFLAGS%" -o scansvc.exe .
) else (
    go build -o scansvc.exe .
)
if errorlevel 1 (
    echo [ERROR] go build failed.
    popd
    goto fail
)
popd
echo.
:skipgo

rem ---------------- 3. package ----------------
if not exist "%OUT%" mkdir "%OUT%"

call :copyto "%SVC%\scansvc.exe"           "scansvc.exe"           1
call :copyto "%SVC%\TWAIN_APP_CMD64.dll"   "TWAIN_APP_CMD64.dll"   1
call :copyto "%FREEIMAGE%"                 "FreeImage.dll"         1
call :copyto "%SVC%\scansvc-tools.bat"     "scansvc-tools.bat"     1
call :copyto "%SVC%\scansvc-tools.ps1"     "scansvc-tools.ps1"     1

rem TWAINDSM.dll is not in this repo: it comes from the TWAIN DSM installer.
rem Without it no scanner can be enumerated, so try the usual places.
if not exist "%OUT%\TWAINDSM.dll" (
    if exist "%SVC%\TWAINDSM.dll"          ( copy /y "%SVC%\TWAINDSM.dll" "%OUT%\" >nul )
)
if not exist "%OUT%\TWAINDSM.dll" (
    if exist "%SystemRoot%\twain_64\TWAINDSM.dll" ( copy /y "%SystemRoot%\twain_64\TWAINDSM.dll" "%OUT%\" >nul )
)
if exist "%OUT%\TWAINDSM.dll" (
    echo   ok   TWAINDSM.dll
) else (
    echo   MISSING  TWAINDSM.dll  -- install releases\Twain_App_sample01_*\twainapp.win64.installer.msi
    echo            and copy TWAINDSM.dll into %OUT%  ^(without it no scanner is found^)
)

echo.
echo ============================================================
echo  done -^> %OUT%
echo.
echo  Copy the whole folder to the operator PC and double-click scansvc.exe.
echo  Keep scansvc.conf / scans\ / cache\ there if they already exist.
echo ============================================================
endlocal
exit /b 0

:copyto
rem %1 source  %2 name in output  %3 required(1)
if exist %1 (
    copy /y %1 "%OUT%\%~2" >nul
    echo   ok   %~2
) else (
    if "%~3"=="1" (
        echo   MISSING  %~2   ^(expected at %~1^)
    )
)
exit /b 0

:fail
echo.
echo BUILD FAILED
endlocal
exit /b 1
