
go mod tidy -v 2>er.txt

@if not %errorlevel% == 0 goto m1
@pause
@exit

:m1
@echo Error!
@type er.txt
@pause
