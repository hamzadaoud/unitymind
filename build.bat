@echo off
echo ============================================
echo   UnityMind Build Script for Windows
echo ============================================
echo.

:: Check Go is installed
where go >nul 2>nul
if %errorlevel% neq 0 (
    echo [ERROR] Go is not installed or not in PATH.
    echo Download Go from: https://go.dev/dl/
    echo Install the Windows AMD64 version, then re-run this script.
    pause
    exit /b 1
)

echo [OK] Go found:
go version
echo.

:: Create cache directory
if not exist cache mkdir cache
echo [OK] Cache directory ready.
echo.

:: Build for Windows (current machine)
echo [BUILD] Compiling UnityMind.exe for Windows...
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go build -ldflags="-s -w" -o UnityMind.exe .
if %errorlevel% neq 0 (
    echo [ERROR] Build failed.
    pause
    exit /b 1
)
echo [OK] UnityMind.exe built successfully!
echo.

:: Optional: build for ARM Windows (Surface Pro X, etc.)
echo [BUILD] Compiling UnityMind_arm64.exe for Windows ARM...
set GOARCH=arm64
go build -ldflags="-s -w" -o UnityMind_arm64.exe .
echo [OK] UnityMind_arm64.exe built.
echo.

:: Optional: build for Linux ARM (Raspberry Pi, etc.)
echo [BUILD] Compiling unitymind_linux_arm for Linux ARM...
set GOOS=linux
set GOARCH=arm
set GOARM=6
go build -ldflags="-s -w" -o unitymind_linux_arm .
echo [OK] unitymind_linux_arm built.
echo.

:: Optional: build for Linux x86 64-bit
echo [BUILD] Compiling unitymind_linux_amd64 for Linux x64...
set GOARCH=amd64
set GOOS=linux
go build -ldflags="-s -w" -o unitymind_linux_amd64 .
echo [OK] unitymind_linux_amd64 built.
echo.

echo ============================================
echo   Build Complete!
echo ============================================
echo.
echo   UnityMind.exe          - Windows 64-bit
echo   UnityMind_arm64.exe    - Windows ARM64
echo   unitymind_linux_arm    - Linux ARM (Pi etc)
echo   unitymind_linux_amd64  - Linux x64
echo.
echo   Double-click UnityMind.exe to launch!
echo ============================================
pause
