#!/usr/bin/env python3
"""Test an actual helper snapshot in disposable network namespaces only.

Run under sudo unshare --net, optionally passing the helper source path.
By default use ../public-hairpin-routing.py. Each case uses another fresh netns.
"""
import os
import subprocess
import sys
from pathlib import Path

assert os.readlink('/proc/self/ns/net') != os.readlink('/proc/1/ns/net'), 'Refusing host network namespace'
assert len(sys.argv) <= 2, 'Usage: public-hairpin-faults.py [helper.py]'
helper_source=(Path(sys.argv[1]) if len(sys.argv)==2 else Path(__file__).resolve().parent.parent/'public-hairpin-routing.py').resolve()
assert helper_source.is_file(), f'Helper source is not readable: {helper_source}'

CASE_CODE = r'''
import importlib.util, json, os, subprocess, sys, tempfile
from pathlib import Path
assert os.readlink('/proc/self/ns/net') != os.readlink('/proc/1/ns/net')
case=os.environ['FAULT_CASE']
def command(*args):
    return subprocess.run(args,check=True,text=True,capture_output=True).stdout
command('ip','link','set','lo','up')
command('ip','link','add','wg-users','type','dummy')
command('ip','address','add','10.89.0.1/24','dev','wg-users')
command('ip','link','set','wg-users','up')
command('ip','address','add','195.133.60.194/32','dev','lo')
command('ip','route','add','unreachable','default','table','51889')
with tempfile.TemporaryDirectory(prefix='hairpin-helper-fault.',dir='/tmp') as folder:
    folder=Path(folder)
    source=Path(os.environ['WIREGUARD_TEST_HELPER_SOURCE']).read_bytes()
    path=folder/'helper.py'; path.write_bytes(source)
    spec=importlib.util.spec_from_file_location('helper_under_test',path)
    helper=importlib.util.module_from_spec(spec);spec.loader.exec_module(helper)
    os.environ.update(WIREGUARD_PUBLIC_ADDRESS='195.133.60.194',WIREGUARD_CLIENT_CIDR='10.89.0.0/24',
        WIREGUARD_INGRESS_INTERFACE='wg-users',WIREGUARD_ROUTE_TABLE='51889',
        WIREGUARD_HAIRPIN_STATE_FILE=str(folder/'state.json'),WIREGUARD_HAIRPIN_LOCK_FILE=str(folder/'lock'))
    state=folder/'state.json'
    real_ip=helper.ip
    read_args=('-j','-details','-4','rule','show')
    original_add=('-4','rule','add','priority','0','from','all','lookup','local','protocol','kernel')
    original_del=('-4','rule','del','priority','0','from','all','lookup','local','protocol','kernel')
    local_add=('-4','rule','add','priority','2','from','all','lookup','local','protocol','kernel')
    local_del=('-4','rule','del','priority','2','from','all','lookup','local','protocol','kernel')
    def current(): return json.loads(real_ip(*read_args))
    def low(): return [r for r in current() if r['priority']<=2]
    def local_invariant():
        assert any(r.get('table')=='local' and r.get('src')=='all' and 'dst' not in r and 'iif' not in r
                   for r in current()), 'LOST ALL GENERIC LOCAL LOOKUPS'
    baseline=low()
    def run(mode):
        sys.argv=['helper.py',mode]
        helper.main()
    stopping=case in {'lost_stop_local','failed_stop_restore'}
    if stopping: run('start')
    flags={'pending_read_failure':False,'read_failed':False,'mutation_failed':False,'restore_failed':False}
    def injected_ip(*args):
        if args==read_args and flags['pending_read_failure']:
            flags['pending_read_failure']=False;flags['read_failed']=True
            raise subprocess.CalledProcessError(91,['ip',*args],stderr='injected final readback failure')
        if flags['read_failed'] and not flags['restore_failed'] and (
            (case=='failed_start_restore' and args==original_add) or
            (case=='failed_stop_restore' and args==local_add)):
            flags['restore_failed']=True
            raise subprocess.CalledProcessError(92,['ip',*args],stderr='injected rollback restore failure')
        result=real_ip(*args)
        mutation=len(args)>2 and args[0:2]==('-4','rule') and args[2] in {'add','del'}
        if mutation:
            local_invariant()
            if not flags['mutation_failed'] and (
                (case=='lost_start_original' and args==original_del) or
                (case=='lost_start_lookup' and args[2]=='add' and '242' in args and 'unreachable' not in args) or
                (case=='lost_stop_local' and args==local_del)):
                flags['mutation_failed']=True
                raise subprocess.CalledProcessError(93,['ip',*args],stderr='injected error AFTER successful netlink mutation')
            if not flags['read_failed'] and (
                (case=='failed_start_restore' and args==original_del) or
                (case=='failed_stop_restore' and args==local_del)):
                flags['pending_read_failure']=True
        return result
    helper.ip=injected_ip
    try:
        run('stop' if stopping else 'start')
        raise AssertionError('Expected injected operation failure')
    except subprocess.CalledProcessError:
        pass
    local_invariant()
    result=low()
    if case in {'lost_start_original','lost_start_lookup'}:
        assert result==baseline,(case,result)
        assert not state.exists(),(case,'unexpected state file')
    elif case=='lost_stop_local':
        assert len(result)==3 and any(r.get('protocol')=='242' for r in result),result
        assert state.exists()
    elif case=='failed_start_restore':
        assert result==[{'priority':2,'src':'all','table':'local','protocol':'kernel'}],result
        assert state.exists()
    elif case=='failed_stop_restore':
        assert any(r.get('priority')==0 and r.get('table')=='local' for r in result),result
        assert not any(r.get('priority')==2 for r in result),result
        assert state.exists()
    assert flags['mutation_failed'] or (flags['read_failed'] and flags['restore_failed']),flags
    print('PASS',case,'safe residual rules',json.dumps(result,sort_keys=True),flush=True)
    helper.ip=real_ip
    run('stop')
    assert low()==baseline
    assert not state.exists()
    print('PASS',case,'normal recovery to original state',flush=True)
'''

for case in ('lost_start_original','lost_start_lookup','lost_stop_local','failed_start_restore','failed_stop_restore'):
    env=os.environ.copy();env['FAULT_CASE']=case
    env['WIREGUARD_TEST_HELPER_SOURCE']=str(helper_source)
    subprocess.run(['unshare','--net','python3','-c',CASE_CODE],check=True,env=env,timeout=20)
print('PASS: all actual-helper unknown-outcome and rollback-failure cases preserve local routing')
