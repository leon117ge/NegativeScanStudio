#!/usr/bin/env python3
"""Compare parameter definitions of the Windows/Linux build and the macOS UI variant.
Usage: check_param_defs.py <win_linux_dump> <mac_dump>
- Windows/Linux keeps the 0.90.14 Rendering controls exactly: group, Render engine and Show render
  engine hidden, choices 'Auto (CUDA/OpenCL if offered)|CPU', default 0.
- macOS differs ONLY there: the three are visible, choices exactly 'Auto (Metal)|CPU' (same indices:
  0 = Auto, 1 = CPU), default 0, plus hints. Every other parameter is identical, in the same order."""
import sys
def load(path):
 params,order,cur={}, [],None
 for line in open(path,encoding='utf-8'):
  line=line.rstrip('\n')
  if line.startswith('param '):
   cur=line.split()[1];assert cur not in params,f'duplicate parameter {cur}';order.append(cur);params[cur]=[line]
  else:params[cur].append(line)
 return order,params
wo,wp=load(sys.argv[1]);mo,mp=load(sys.argv[2])
assert wo==mo,'parameter names or order differ between platforms'
SECRET='  int OfxParamPropSecret = 1'
def prop(lines,key):return [l for l in lines if l.split(' = ')[0].split()[-1]==key]
RENDER={'render','render_device','render_info'}
for name in wo:
 if name not in RENDER:assert wp[name]==mp[name],f'{name} differs between Windows/Linux and macOS';continue
 assert SECRET in wp[name],f'Windows/Linux: {name} must stay hidden'
 assert SECRET not in mp[name],f'macOS: {name} must be visible'
assert prop(wp['render_device'],'OfxParamPropChoiceOption')==['  string OfxParamPropChoiceOption = [Auto (CUDA/OpenCL if offered)] [CPU]']
assert prop(mp['render_device'],'OfxParamPropChoiceOption')==['  string OfxParamPropChoiceOption = [Auto (Metal)] [CPU]']
for p in (wp,mp):
 assert prop(p['render_device'],'OfxParamPropDefault')==['  int OfxParamPropDefault = 0']
 assert prop(p['render_device'],'OfxParamPropParent')==prop(p['render_info'],'OfxParamPropParent')==['  string OfxParamPropParent = [render]']
for name in RENDER:
 strip=lambda lines:[l for l in lines if l!=SECRET and not l.startswith('  string OfxParamPropHint') and not l.startswith('  string OfxParamPropChoiceOption')]
 assert strip(wp[name])==strip(mp[name]),f'{name}: only visibility, choice labels and hint may differ'
print(f'PASS: {len(wo)} unique parameters; Windows/Linux Rendering controls hidden as in 0.90.14; macOS shows Render engine (Auto (Metal)|CPU, indices 0/1) and Show render engine; nothing else differs')
