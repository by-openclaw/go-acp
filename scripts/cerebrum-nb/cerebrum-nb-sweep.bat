@echo off
setlocal

set HOST=10.41.64.95
set PORT=40008
set DHS_CEREBRUM_USER=tallyadm
set DHS_CEREBRUM_PASS=Tallyadm1234!
set OUT=details-95.log

if exist %OUT% del %OUT%

call :details 10.5.6.20
call :details 1.0.0.2
call :details 10.41.40.205
call :details 10.41.63.59
call :details 10.41.69.150
call :details 10.41.63.60
call :details 10.41.63.131
call :details 10.41.40.58
call :details 10.41.40.59
call :details 10.41.40.175
call :details 10.107.30.100
call :details 1.0.0.0
call :details 10.80.4.13
call :details 10.41.63.15
call :details 10.41.64.94
call :details 10.80.4.14
call :details 10.41.69.90
call :details 10.80.4.10
call :details 10.41.40.10
call :details 10.41.40.139
call :details 10.41.39.132
call :details 1.1.1.1
call :details 10.41.69.11
call :details 10.80.4.12
call :details 1.1.1.2
call :details 10.44.57.42
call :details 10.41.40.171

echo.
echo ====================================================
echo SUMMARY (vendor + name per IP)
echo ====================================================
findstr /R /C:"^=== " /C:"^name " /C:"^vendor " %OUT%

endlocal
goto :eof

:details
echo === %~1 === >> %OUT%
dhs.exe consumer cerebrum-nb device-details %HOST% --port %PORT% --device %~1 --device-type DEVICE >> %OUT% 2>&1
echo === %~1 ===
goto :eof