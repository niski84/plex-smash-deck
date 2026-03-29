Set WshShell = CreateObject("WScript.Shell")
WshShell.Run Chr(34) & WScript.Arguments(0) & Chr(34), 0
