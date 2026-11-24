; The installer of WoW Forever Tools: the exe and the names file of the writs (scripts/data, which the bridge
; reads next to the exe), for the current user, without administrator rights.
; Built by build.sh --installer: makensis -DVERSION=<x.y.z> installer.nsi writes
; build/bin/WoW-Forever-Tools-Setup-<x.y.z>.exe (-DOUTFILE=<path> writes it elsewhere).
;
; Switches (what the app's updater passes, update.go):
;   /S            silent: no pages
;   --updated     an update: the shortcuts stay as the user left them
;   --force-run   start the app when done (with --updated, so it says "just updated")
;   /D=<folder>   another folder; the last switch on the line. Without it an update goes where the app is.
Unicode true
ManifestDPIAware true

!ifndef VERSION
  !error "VERSION is not set"
!endif
; Paths are relative to this script's folder (makensis changes into it) and written with backslashes: the
; makensis of Windows does not find a file given with forward slashes (CI, 2026-11-23); the one of Linux turns
; them into slashes.
!define APPDIR ".."
!ifndef OUTFILE
  !define OUTFILE "${APPDIR}\build\bin\WoW-Forever-Tools-Setup-${VERSION}.exe"
!endif
!define APP "WoW Forever Tools"
!define EXE "WoW Forever Tools.exe"
!define REGKEY "Software\WoW Forever Tools"
!define UNINSTKEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\WoWForeverTools"

!include "MUI2.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"

Name "${APP}"
OutFile "${OUTFILE}"
InstallDir "$LOCALAPPDATA\Programs\${APP}"
InstallDirRegKey HKCU "${REGKEY}" "InstallDir"
RequestExecutionLevel user
SetCompressor /SOLID lzma
BrandingText "${APP} ${VERSION}"

VIProductVersion "${VERSION}.0"
VIAddVersionKey /LANG=0 "ProductName" "${APP}"
VIAddVersionKey /LANG=0 "ProductVersion" "${VERSION}"
VIAddVersionKey /LANG=0 "FileVersion" "${VERSION}"
VIAddVersionKey /LANG=0 "FileDescription" "${APP} Setup"
VIAddVersionKey /LANG=0 "CompanyName" "kejith"
VIAddVersionKey /LANG=0 "LegalCopyright" "Copyright (C) 2026 kejith"

!define MUI_ICON "${APPDIR}\build\icon.ico"
!define MUI_UNICON "${APPDIR}\build\icon.ico"
!define MUI_ABORTWARNING
!define MUI_FINISHPAGE_RUN "$INSTDIR\${EXE}"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
; NSIS shows the language of Windows if it is one of these.
!insertmacro MUI_LANGUAGE "English"
!insertmacro MUI_LANGUAGE "German"

Var Updated
Var ForceRun

Function .onInit
  ${GetParameters} $0
  ClearErrors
  ${GetOptions} $0 "--updated" $1
  ${IfNot} ${Errors}
    StrCpy $Updated 1
  ${EndIf}
  ClearErrors
  ${GetOptions} $0 "--force-run" $1
  ${IfNot} ${Errors}
    StrCpy $ForceRun 1
  ${EndIf}
FunctionEnd

; The app quits right after it started the installer of an update; wait up to 20 s until Windows lets go of
; its file.
Function WaitForApp
  StrCpy $R0 0
  ${DoWhile} $R0 < 40
    ${IfNot} ${FileExists} "$INSTDIR\${EXE}"
      Return
    ${EndIf}
    ClearErrors
    FileOpen $R1 "$INSTDIR\${EXE}" a
    ${IfNot} ${Errors}
      FileClose $R1
      Return
    ${EndIf}
    Sleep 500
    IntOp $R0 $R0 + 1
  ${Loop}
FunctionEnd

Section "Install"
  SetOutPath "$INSTDIR"
  Call WaitForApp
  File "/oname=${EXE}" "${APPDIR}\build\bin\${EXE}"
  SetOutPath "$INSTDIR\scripts\data"
  File "${APPDIR}\..\scripts\data\item_names.json"
  SetOutPath "$INSTDIR"
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  WriteRegStr HKCU "${REGKEY}" "InstallDir" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayName" "${APP}"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINSTKEY}" "Publisher" "kejith"
  WriteRegStr HKCU "${UNINSTKEY}" "URLInfoAbout" "https://github.com/kejith/wow-forever-tools"
  WriteRegStr HKCU "${UNINSTKEY}" "DisplayIcon" "$INSTDIR\${EXE}"
  WriteRegStr HKCU "${UNINSTKEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINSTKEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "${UNINSTKEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINSTKEY}" "NoRepair" 1
  ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
  IntFmt $0 "0x%08X" $0
  WriteRegDWORD HKCU "${UNINSTKEY}" "EstimatedSize" "$0"

  ${If} $Updated != 1
    CreateShortcut "$SMPROGRAMS\${APP}.lnk" "$INSTDIR\${EXE}"
    CreateShortcut "$DESKTOP\${APP}.lnk" "$INSTDIR\${EXE}"
  ${EndIf}
  ${If} $ForceRun == 1
    Exec '"$INSTDIR\${EXE}" --updated'
  ${EndIf}
SectionEnd

; Settings, downloaded addons and game data stay in %APPDATA%\wowmcp-app.
Section "Uninstall"
  Delete "$INSTDIR\${EXE}"
  Delete "$INSTDIR\scripts\data\item_names.json"
  RMDir "$INSTDIR\scripts\data"
  RMDir "$INSTDIR\scripts"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\${APP}.lnk"
  Delete "$DESKTOP\${APP}.lnk"
  DeleteRegKey HKCU "${UNINSTKEY}"
  DeleteRegKey HKCU "${REGKEY}"
SectionEnd
