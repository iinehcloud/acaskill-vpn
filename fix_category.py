import json

path = r'C:\Users\P4\Downloads\files (2)\gui\src-tauri\tauri.conf.json'
cfg = json.load(open(path))

# Valid Tauri categories: https://tauri.app/reference/config/#category
# Must be one of the macOS/freedesktop standard categories
cfg['bundle']['category'] = "Utility"  # valid Tauri category

json.dump(cfg, open(path, 'w'), indent=2)
print('Fixed category to Utility')
print(json.dumps(cfg['bundle'], indent=2))
