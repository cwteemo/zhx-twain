@echo off
rem ============================================================
rem  scansvc build script -- full docs in BUILD.md (same folder)
rem
rem  USAGE
rem    build.bat                 build DLL + service (32-bit), package into dist\x86\
rem    build.bat -x64            64-bit build instead, into dist\x64\
rem    build.bat dll             only the TWAIN DLL (Visual Studio / MSBuild)
rem    build.bat go              only scansvc.exe (Go); needs the DLL built before
rem    build.bat -debug          Debug build of the DLL (default: Release)
rem    build.bat -console        keep the console window (default: no window, tray only)
rem    build.bat -notest         skip Go unit tests
rem    build.bat -out D:\deploy  package into another folder (no arch subfolder added)
rem    flags can be combined:    build.bat -x64 -debug -console -out D:\test
rem
rem  32-BIT OR 64-BIT?
rem    A TWAIN driver (.ds) is a DLL loaded into this very process, so service,
rem    DLL, DSM and driver must all have the same bitness. Scanners whose vendor
rem    only ships a 32-bit driver (several older Kodak models) cannot be
rem    enumerated by the 64-bit service at all. That is why 32-bit is the default:
rem    nearly every vendor ships a 32-bit driver. Use -x64 only for a scanner that
rem    has nothing but a 64-bit driver.
rem    Same sources for both; GET /api/diagnose on a running service tells you
rem    which drivers are installed for which bitness.
rem
rem  REQUIREMENTS on the build machine
rem    Visual Studio 2017+ with the "Desktop development with C++" workload
rem      (MSBuild is located with vswhere, no need for a Developer Command Prompt)
rem    Go 1.19+
rem    MinGW-w64 gcc for cgo, matching the target bitness -- check: gcc -dumpmachine
rem      64-bit build -> x86_64-w64-mingw32
rem      32-bit build -> i686-w64-mingw32 (i686-w64-mingw32-gcc.exe is picked up
rem                      automatically when it is in PATH)
rem
rem  WHAT IT DOES
rem    1. MSBuild the DLL, copy TWAIN_APP_CMD<32|64>.dll/.lib next to the Go sources
rem       (.lib is only needed to link; it is NOT shipped)
rem    2. go test ./imgfmt ./scanopt ./twaindiag  (pure-Go packages; main needs the DLL)
rem    3. go build scansvc.exe
rem    4. copy everything the service needs at runtime into the output folder
rem
rem  OUTPUT (dist\x86\ or dist\x64\)
rem    scansvc.exe  TWAIN_APP_CMD<32|64>.dll  FreeImage.dll  TWAINDSM.dll
rem    scansvc-tools.bat  scansvc-tools.ps1
rem    Copy the whole folder to the operator PC. When upgrading, keep the files
rem    already there: scansvc.conf, scans\, cache\, scanner-options.jsonc
rem    Never mix the two folders: FreeImage.dll and TWAINDSM.dll have the same
rem    name in both but different bitness.
rem
rem  Any failing step stops the script and prints why.
rem ============================================================

setlocal EnableDelayedExpansion

set "ROOT=%~dp0"
set "SLN=%ROOT%TWAIN-Samples\Twain_App_sample01\visual_studio\TWAIN_APP_VS2017.sln"
set "DLLOUT=%ROOT%TWAIN-Samples\Twain_App_sample01\visual_studio"
set "SVC=%ROOT%TWAIN-Samples\Twain_App_sample01\src\scansvc"

set "CONFIG=Release"
set "LDFLAGS=-H=windowsgui"
set "OUT="
set "ARCH=x86"
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
if /i "%~1"=="-x86"      ( set "ARCH=x86" & shift & goto parse )
if /i "%~1"=="-32"       ( set "ARCH=x86" & shift & goto parse )
if /i "%~1"=="-x64"      ( set "ARCH=x64" & shift & goto parse )
if /i "%~1"=="-64"       ( set "ARCH=x64" & shift & goto parse )
echo [ERROR] unknown argument: %~1
echo.
echo   usage: build.bat [all^|dll^|go] [-x86^|-x64] [-debug] [-console] [-notest] [-out ^<dir^>]
goto fail
:parsed

rem ---------------- everything that differs between the two bitnesses ----------------
rem One set of sources, two outputs. The only thing the Go code needs to know is
rem which DLL to link, and that is decided by link_windows_386.go / _amd64.go.
rem MSPLAT is the SOLUTION platform: the .sln calls 32-bit "x86" and maps it to the
rem project's "Win32" itself; passing Win32 to the .sln fails with MSB4126
if /i "%ARCH%"=="x86" (
    set "MSPLAT=x86"
    set "DLLNAME=TWAIN_APP_CMD32"
    set "GOARCHVAL=386"
    set "GCCWANT=i686"
    set "SYSO=rsrc_windows_386.syso"
    set "FREEIMAGE=%ROOT%TWAIN-Samples\pub\external\bin\win32\FreeImage.dll"
    set "DSMDIR=twain_32"
    set "DSMSYS=SysWOW64"
    set "MSIHINT=twainapp.win32.installer.msi"
) else (
    set "MSPLAT=x64"
    set "DLLNAME=TWAIN_APP_CMD64"
    set "GOARCHVAL=amd64"
    set "GCCWANT=x86_64"
    set "SYSO=rsrc_windows_amd64.syso"
    set "FREEIMAGE=%ROOT%TWAIN-Samples\pub\external\bin\win64\FreeImage.dll"
    set "DSMDIR=twain_64"
    set "DSMSYS=System32"
    set "MSIHINT=twainapp.win64.installer.msi"
)
if not defined OUT set "OUT=%ROOT%dist\%ARCH%"

echo ============================================================
echo  config : %CONFIG% ^| %MSPLAT%
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
"%MSBUILD%" "%SLN%" /t:Build /p:Configuration=%CONFIG% /p:Platform=%MSPLAT% /m /nologo /v:minimal
if errorlevel 1 (
    echo [ERROR] DLL build failed. See the messages above.
    goto fail
)

if not exist "%DLLOUT%\%CONFIG%\%DLLNAME%.dll" (
    echo [ERROR] built, but %DLLNAME%.dll is missing in %DLLOUT%\%CONFIG%
    goto fail
)
rem cgo links against the .lib and loads the .dll, both must sit next to the Go sources
copy /y "%DLLOUT%\%CONFIG%\%DLLNAME%.dll" "%SVC%\" >nul
copy /y "%DLLOUT%\%CONFIG%\%DLLNAME%.lib" "%SVC%\" >nul
echo       %DLLNAME%.dll/.lib -^> src\scansvc\
echo.
:skipdll

rem ---------------- 2. scansvc.exe ----------------
if "%DO_GO%"=="0" goto skipgo

where go.exe >nul 2>nul
if errorlevel 1 (
    echo [ERROR] go.exe not found in PATH. Install Go 1.19+.
    goto fail
)
rem a 32-bit cross compiler under its full name is preferred for -x86 builds:
rem plain "gcc" on a normal MinGW-w64 install is 64-bit only and cannot make a
rem 32-bit object, so cgo would fail deep inside the link with confusing errors
set "CC="
if /i "%ARCH%"=="x86" call :findcc32
if not defined CC (
    where gcc.exe >nul 2>nul
    if errorlevel 1 (
        echo [ERROR] gcc.exe not found in PATH. cgo needs MinGW-w64 ^(%GCCWANT%^).
        goto fail
    )
    set "CCBIN=gcc"
) else (
    set "CCBIN=%CC%"
)
rem bitness mismatch is the classic trap: it compiles but cannot link the DLL
set "GCCARCH="
for /f "usebackq tokens=*" %%i in (`"!CCBIN!" -dumpmachine`) do set "GCCARCH=%%i"
echo !GCCARCH! | findstr /i "%GCCWANT%" >nul
if errorlevel 1 (
    echo [ERROR] the C compiler is "!GCCARCH!", but a %ARCH% build needs %GCCWANT%.
    echo         Install the matching MinGW-w64 and put its bin folder first in PATH.
    if /i "%ARCH%"=="x86" echo         For 32-bit, i686-w64-mingw32-gcc.exe is picked up automatically.
    goto fail
)
if not exist "%SVC%\%DLLNAME%.lib" (
    echo [ERROR] %SVC%\%DLLNAME%.lib is missing.
    echo         Build the DLL first:  build.bat dll -%ARCH%
    goto fail
)

pushd "%SVC%"
set CGO_ENABLED=1
set GOARCH=%GOARCHVAL%
set GOOS=windows

if "%DO_TEST%"=="1" (
    echo [2/3] running tests ...
    rem only the pure-Go packages: the main package needs the DLL and cannot be tested here
    go test ./imgfmt ./scanopt ./twaindiag
    if errorlevel 1 (
        echo [ERROR] tests failed. Build stopped.
        popd
        goto fail
    )
    echo.
)

rem the exe icon lives in a .syso resource file; regenerate it when it is missing.
rem it is per-architecture (the COFF machine type must match), hence %SYSO%
rem (changing favicon.ico? run: go run ./tools/mkicon favicon.ico %SYSO%)
if not exist "%SYSO%" (
    if exist "favicon.ico" (
        echo       generating icon resource from favicon.ico ...
        go run ./tools/mkicon favicon.ico "%SYSO%"
    )
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
call :copyto "%SVC%\%DLLNAME%.dll"        "%DLLNAME%.dll"         1
call :copyto "%FREEIMAGE%"                 "FreeImage.dll"         1
call :copyto "%SVC%\scansvc-tools.bat"     "scansvc-tools.bat"     1
call :copyto "%SVC%\scansvc-tools.ps1"     "scansvc-tools.ps1"     1

rem TWAINDSM.dll is not in this repo: it comes from the TWAIN DSM installer.
rem Without it no scanner can be enumerated, so try the usual places, in order:
rem   1. next to the Go sources (drop one there by hand to pin a version)
rem   2. the system folder the DSM installer uses: System32 for 64-bit,
rem      SysWOW64 for 32-bit (on a 32-bit Windows that is System32 again)
rem   3. the twain_32 / twain_64 driver folder, where some vendors put a copy
rem a TWAINDSM.dll already in the output folder is kept as is
set "DSMFROM="
if exist "%OUT%\TWAINDSM.dll" set "DSMFROM=%OUT%, kept"
if /i "%ARCH%"=="x86" if not exist "%SystemRoot%\SysWOW64\" set "DSMSYS=System32"
for %%d in ("%SVC%" "%SystemRoot%\%DSMSYS%" "%SystemRoot%\%DSMDIR%") do (
    if not defined DSMFROM if exist "%%~d\TWAINDSM.dll" (
        copy /y "%%~d\TWAINDSM.dll" "%OUT%\" >nul
        set "DSMFROM=%%~d"
    )
)
if defined DSMFROM (
    echo   ok   TWAINDSM.dll  ^(%ARCH%, from !DSMFROM!^)
) else (
    echo   MISSING  TWAINDSM.dll  -- install releases\Twain_App_sample01_*\%MSIHINT%
    echo            or copy the %ARCH% TWAINDSM.dll into %OUT%  ^(without it no scanner is found^)
    echo            looked in: %SVC%  %SystemRoot%\%DSMSYS%  %SystemRoot%\%DSMDIR%
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

:findcc32
rem use the i686 cross compiler for 32-bit builds when it is available
where i686-w64-mingw32-gcc.exe >nul 2>nul
if not errorlevel 1 set "CC=i686-w64-mingw32-gcc"
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
