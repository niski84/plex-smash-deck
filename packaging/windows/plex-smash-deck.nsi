; plex-smash-deck Windows installer (NSIS)
; Builds a per-user install under %LOCALAPPDATA% so runtime data writes work.

!include "MUI2.nsh"

!ifndef APP_VERSION
  !define APP_VERSION "dev"
!endif
!ifndef SOURCE_DIR
  !define SOURCE_DIR "dist\winpkg"
!endif
!ifndef OUT_FILE
  !define OUT_FILE "dist\plex-smash-deck-${APP_VERSION}-windows-x64-setup.exe"
!endif

Name "Plex Smash Deck"
OutFile "${OUT_FILE}"
InstallDir "$LOCALAPPDATA\Plex Smash Deck"
InstallDirRegKey HKCU "Software\PlexSmashDeck" "InstallDir"
RequestExecutionLevel user

!define MUI_ABORTWARNING
!define MUI_ICON "${NSISDIR}\Contrib\Graphics\Icons\modern-install.ico"
!define MUI_UNICON "${NSISDIR}\Contrib\Graphics\Icons\modern-uninstall.ico"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_UNPAGE_FINISH

!insertmacro MUI_LANGUAGE "English"

Var StartMenuFolder

; Upgrade: same InstallDir + Add/Remove Programs key as previous releases.
; We overwrite files in place (no silent uninstall first) so data next to the exe is preserved.
Function .onInit
  ReadRegStr $0 HKCU "Software\PlexSmashDeck" "InstallDir"
  StrCmp $0 "" initDone
  StrCpy $INSTDIR $0
  IfSilent initDone
  MessageBox MB_OK|MB_ICONINFORMATION "A previous installation of Plex Smash Deck was found.$\r$\n$\r$\nSetup will upgrade it in place. Settings and data in the install folder are kept."
initDone:
FunctionEnd

Section "Install Core Files" SecCore
  SetOverwrite on
  SetOutPath "$INSTDIR"

  ; Replace bundled web UI cleanly (removes files dropped from a newer layout).
  RMDir /r "$INSTDIR\web"

  ; app + static web assets
  File /r "${SOURCE_DIR}\*.*"

  ; helper launcher scripts
  FileOpen $0 "$INSTDIR\run-plex-smash-deck.bat" w
  FileWrite $0 "@echo off$\r$\n"
  FileWrite $0 "cd /d %~dp0$\r$\n"
  FileWrite $0 "start \"\" /min plex-dashboard.exe$\r$\n"
  FileClose $0

  FileOpen $0 "$INSTDIR\start-hidden.vbs" w
  FileWrite $0 "Set WshShell = CreateObject(""WScript.Shell"")$\r$\n"
  FileWrite $0 "WshShell.Run Chr(34) ^& WScript.Arguments(0) ^& Chr(34), 0$\r$\n"
  FileClose $0

  ; start menu entries
  StrCpy $StartMenuFolder "$SMPROGRAMS\Plex Smash Deck"
  CreateDirectory "$StartMenuFolder"
  CreateShortcut "$StartMenuFolder\Plex Smash Deck (start server).lnk" "$WINDIR\System32\wscript.exe" '"$INSTDIR\start-hidden.vbs" "$INSTDIR\run-plex-smash-deck.bat"' "$INSTDIR\plex-dashboard.exe" 0
  WriteINIStr "$StartMenuFolder\Open UI.url" "InternetShortcut" "URL" "http://127.0.0.1:8081/"
  CreateShortcut "$StartMenuFolder\Uninstall Plex Smash Deck.lnk" "$INSTDIR\Uninstall.exe"

  ; Add/Remove Programs entry
  WriteRegStr HKCU "Software\PlexSmashDeck" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\PlexSmashDeck" "DisplayName" "Plex Smash Deck"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\PlexSmashDeck" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\PlexSmashDeck" "DisplayVersion" "${APP_VERSION}"
  WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\PlexSmashDeck" "Publisher" "plex-smash-deck"
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\PlexSmashDeck" "NoModify" 1
  WriteRegDWORD HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\PlexSmashDeck" "NoRepair" 1

  WriteUninstaller "$INSTDIR\Uninstall.exe"
SectionEnd

Section /o "Run at login (background)" SecStartup
  CreateShortcut "$SMSTARTUP\Plex Smash Deck.lnk" "$WINDIR\System32\wscript.exe" '"$INSTDIR\start-hidden.vbs" "$INSTDIR\run-plex-smash-deck.bat"' "$INSTDIR\plex-dashboard.exe" 0
SectionEnd

Section "Uninstall"
  Delete "$SMSTARTUP\Plex Smash Deck.lnk"
  Delete "$SMPROGRAMS\Plex Smash Deck\Plex Smash Deck (start server).lnk"
  Delete "$SMPROGRAMS\Plex Smash Deck\Open UI.url"
  Delete "$SMPROGRAMS\Plex Smash Deck\Uninstall Plex Smash Deck.lnk"
  RMDir "$SMPROGRAMS\Plex Smash Deck"

  DeleteRegKey HKCU "Software\Microsoft\Windows\CurrentVersion\Uninstall\PlexSmashDeck"
  DeleteRegKey HKCU "Software\PlexSmashDeck"

  Delete "$INSTDIR\Uninstall.exe"
  Delete "$INSTDIR\run-plex-smash-deck.bat"
  Delete "$INSTDIR\start-hidden.vbs"
  RMDir /r "$INSTDIR"
SectionEnd
