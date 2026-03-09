!macro NSIS_HOOK_POSTINSTALL
    ; Install and start the AcaSkill VPN daemon service
    ; NSIS already runs as admin, so this works without UAC
    nsExec::ExecToLog '"$INSTDIR\acaskill-daemon.exe" install'
    nsExec::ExecToLog '"$WINDIR\System32\sc.exe" config AcaSkillVPN start= auto'
    nsExec::ExecToLog '"$WINDIR\System32\sc.exe" start AcaSkillVPN'
!macroend

!macro NSIS_HOOK_PREUNINSTALL
    ; Stop and remove the daemon service before uninstalling
    nsExec::ExecToLog '"$WINDIR\System32\sc.exe" stop AcaSkillVPN'
    Sleep 2000
    nsExec::ExecToLog '"$INSTDIR\acaskill-daemon.exe" uninstall'
!macroend
