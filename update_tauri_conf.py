import json

path = r'C:\Users\P4\Downloads\files (2)\gui\src-tauri\tauri.conf.json'
cfg = json.load(open(path))

# Add resources - daemon and CLI will be copied alongside the GUI exe
cfg['bundle']['resources'] = {
    "../../client-windows/build/acaskill-daemon.exe": "acaskill-daemon.exe",
    "../../client-windows/build/acaskill-cli.exe": "acaskill-cli.exe"
}

# Add Windows-specific installer config
cfg['bundle']['windows'] = {
    "nsis": {
        "installMode": "perMachine",
        "languages": ["English"],
        "displayLanguageSelector": False
    },
    "wix": None
}

# Add publisher info
cfg['bundle']['publisher'] = "AcaSkill"
cfg['bundle']['copyright'] = "Copyright (c) 2026 AcaSkill"
cfg['bundle']['category'] = "Network"
cfg['bundle']['shortDescription'] = "Bond multiple internet connections into one fast VPN"
cfg['bundle']['longDescription'] = "AcaSkill VPN bonds multiple network interfaces (Ethernet, Wi-Fi, Mobile) into a single high-speed encrypted tunnel, maximizing your available bandwidth."

json.dump(cfg, open(path, 'w'), indent=2)
print('tauri.conf.json updated')
print('Resources to be bundled:')
for src, dst in cfg['bundle']['resources'].items():
    print(f'  {src} -> {dst}')
