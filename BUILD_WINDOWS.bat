@echo off
setlocal
cd /d "%~dp0"
where go >nul 2>nul || (echo Go 1.23 or newer is required. && exit /b 1)
go test ./internal/...
if errorlevel 1 exit /b 1
set GOOS=windows
set GOARCH=386
set CGO_ENABLED=0
go build -ldflags "-H=windowsgui -s -w" -o "Frosty Zugbonk.exe" .
if errorlevel 1 exit /b 1
echo Built Frosty Zugbonk.exe
