@echo off

set GOARCH=amd64
set GOOS=linux
set CGO_ENABLED=0

echo Get version...
if not exist build.ver (
    echo Not found build.ver
    goto :EOF
)
set /p version=< build.ver
echo Version : %version%
if ["%version%"]==[""] (
    echo Please check build.ver first.
    goto :EOF
)
echo Build ...
go build -installsuffix cgo -ldflags "-extldflags -static -X 'main.version=%version%'" -a .
echo Done.

