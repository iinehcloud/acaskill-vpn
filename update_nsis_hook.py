import json

path = r'C:\Users\P4\Downloads\files (2)\gui\src-tauri\tauri.conf.json'
cfg = json.load(open(path))

# NSIS runs as admin already, so we can exec the daemon install directly
# Use nsis preinstall/postinstall hooks
cfg['bundle']['windows']['nsis']['installerHooks'] = "../../installer-hooks.nsh"

json.dump(cfg, open(path, 'w'), indent=2)
print('Updated tauri.conf.json with NSIS hook')
