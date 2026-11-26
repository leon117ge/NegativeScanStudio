#!/usr/bin/env python3
"""Compare parameter definitions per platform and build (one mechanism: ofx/src/platform.hpp).
Usage: check_param_defs.py <win_linux_dump> <mac_metal_dump> [<mac_cpu_only_dump>]
- Windows/Linux keeps the 0.90.14 Rendering controls exactly: group, Render engine and Show render
  engine hidden, choices 'Auto (CUDA/OpenCL if offered)|CPU', default 0.
- macOS differs ONLY there: the three are visible, plus hints. Choices are exactly
  'Auto (Metal)|CPU' in a Metal build (-DNSS_METAL) and 'Auto|CPU' in a CPU-only macOS build.
- Indices are the same everywhere: 0 = Auto, 1 = CPU, default 0 (Auto).
Every other parameter is identical, in the same order."""
import sys
def load(path):
 params,order,cur={}, [],None
 for line in open(path,encoding='utf-8'):
  line=line.rstrip('\n')
  if line.startswith('param '):
   cur=line.split()[1];assert cur not in params,f'duplicate parameter {cur}';order.append(cur);params[cur]=[line]
  else:params[cur].append(line)
 return order,params
SECRET='  int OfxParamPropSecret = 1'
RENDER={'render','render_device','render_info'}
def prop(lines,key):return [l for l in lines if l.split(' = ')[0].split()[-1]==key]
def choices(labels):return ['  string OfxParamPropChoiceOption = '+' '.join(f'[{x}]' for x in labels)]
def common(p):
 assert prop(p['render_device'],'OfxParamPropDefault')==['  int OfxParamPropDefault = 0'],'default must be index 0 (Auto)'
 assert prop(p['render_device'],'OfxParamPropParent')==prop(p['render_info'],'OfxParamPropParent')==['  string OfxParamPropParent = [render]']
wo,wp=load(sys.argv[1])
WIN=['Auto (CUDA/OpenCL if offered)','CPU']
for name in RENDER:assert SECRET in wp[name],f'Windows/Linux: {name} must stay hidden'
assert prop(wp['render_device'],'OfxParamPropChoiceOption')==choices(WIN)
common(wp)
rows=[('Windows/Linux',WIN,'hidden')]
for path,label,build in [(sys.argv[2],'Auto (Metal)','macOS Metal')]+([(sys.argv[3],'Auto','macOS CPU-only')] if len(sys.argv)>3 else []):
 mo,mp=load(path)
 assert wo==mo,f'{build}: parameter names or order differ from Windows/Linux'
 for name in wo:
  if name not in RENDER:assert wp[name]==mp[name],f'{build}: {name} differs from Windows/Linux';continue
  assert SECRET not in mp[name],f'{build}: {name} must be visible'
 assert prop(mp['render_device'],'OfxParamPropChoiceOption')==choices([label,'CPU']),f'{build}: choices must be [{label}] [CPU]'
 common(mp)
 for name in RENDER:
  strip=lambda lines:[l for l in lines if l!=SECRET and not l.startswith('  string OfxParamPropHint') and not l.startswith('  string OfxParamPropChoiceOption')]
  assert strip(wp[name])==strip(mp[name]),f'{build}: {name}: only visibility, choice labels and hint may differ'
 rows.append((build,[label,'CPU'],'visible'))
for build,labels,vis in rows:print(f'  {build}: Render engine {vis}, 0 = {labels[0]}, 1 = {labels[1]}, default 0')
print(f'PASS: {len(wo)} unique parameters; Windows/Linux Rendering controls as in 0.90.14; macOS differs only in the Rendering group; indices 0 = Auto, 1 = CPU on every build')
