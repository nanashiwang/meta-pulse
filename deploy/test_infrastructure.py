#!/usr/bin/env python3
import importlib.util
import itertools
import json
import os
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest.mock import patch
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('backup',ROOT/'deploy/backup.py')
b=importlib.util.module_from_spec(spec);spec.loader.exec_module(b)

class BackupTests(unittest.TestCase):
 def test_dsn_and_mysql_options(self):
  cfg=b.parse_dsn('user:p@ss$word"\\@tcp([::1]:3306)/test_db?tls=true&parseTime=true')
  text=b.option_file(cfg,'/etc/meta-pulse-ca/ca.pem').decode()
  self.assertIn('ssl-mode=VERIFY_IDENTITY',text);self.assertEqual(cfg['host'],'::1')
  self.assertIn('ssl-ca=',text)
  for dsn in ['x:p@tcp(db:3306)/db?tls=skip-verify','x:p@tcp(db:3306)/db?tls=preferred','x:p\n@tcp(db:3306)/db','x:p@tcp(db:3306)/db;drop']:
   with self.assertRaises(b.Failure): b.parse_dsn(dsn)
 def test_external_backup_requires_explicit_matching_target(self):
  model={'services':{'pulse-api':{'environment':{'PULSE_DB_DSN':'u:p@tcp(db:3306)/pulse?tls=true'}}}}
  values={'PULSE_DB_MODE':'external'}
  with self.assertRaises(b.Failure):b.database_config('pulse',values,model)
  values['PULSE_BACKUP_DB_DSN']='backup:p@tcp(other:3306)/pulse?tls=true'
  with self.assertRaises(b.Failure):b.database_config('pulse',values,model)
  values['PULSE_BACKUP_DB_DSN']='backup:p@tcp(db:3306)/pulse?tls=false'
  with self.assertRaises(b.Failure):b.database_config('pulse',values,model)
  values['PULSE_BACKUP_DB_DSN']='backup:p@tcp(db:3306)/pulse?tls=true'
  self.assertEqual(b.database_config('pulse',values,model)['user'],'backup')
 def test_local_mode_cannot_back_up_a_different_database(self):
  model={'services':{'pulse-api':{'environment':{'PULSE_DB_DSN':'u:p@tcp(other:3306)/meta_pulse'}},'mysql':{'environment':{'MYSQL_DATABASE':'meta_pulse','MYSQL_ROOT_PASSWORD':'root-secret'}}}}
  with self.assertRaises(b.Failure):b.database_config('pulse',{},model)
  model['services']['pulse-api']['environment']['PULSE_DB_DSN']='u:p@tcp(mysql:3306)/meta_pulse'
  self.assertEqual(b.database_config('pulse',{},model)['user'],'root')
 def fixture(self,root):
  for name in ['pulse.sql','forum.sql','.env','compose.json','runtime-keys/api/key','runtime-keys/worker/key','forum-data/conf/config.yaml']:
   p=root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text('fixture')
  (root/'manifest.json').write_text(json.dumps({'format':1,'files':b.hashes(root),'databases':{'pulse':'pulse','forum':'forum'},'consistency':'quiesced'}))
 def test_verification_detects_missing_corrupt_and_linked_artifacts(self):
  with tempfile.TemporaryDirectory() as tmp:
   root=Path(tmp);self.fixture(root);b.verify(root)
   manifest=json.loads((root/'manifest.json').read_text());manifest['databases']={}
   (root/'manifest.json').write_text(json.dumps(manifest))
   with self.assertRaises(b.Failure):b.verify(root)
   self.fixture(root)
   (root/'pulse.sql').write_text('tampered')
   with self.assertRaises(b.Failure):b.verify(root)
   (root/'pulse.sql').unlink();(root/'pulse.sql').symlink_to('/etc/hosts')
   with self.assertRaises(b.Failure):b.verify(root)
 def test_restart_after_failed_quiesced_backup(self):
  with tempfile.TemporaryDirectory() as tmp:
   root=Path(tmp);env=root/'input.env';env.write_text('test')
   calls=[]
   def compose(*args):
    calls.append(args)
    return 'pulse-api\npulse-worker\nforum' if args[0]=='ps' else ''
   with patch.object(b,'ROOT',root),patch.object(b,'settings',return_value=(env,{},{})),patch.object(b,'database_config'),patch.object(b,'compose',side_effect=compose),patch.object(b,'dump',side_effect=b.Failure('dump failed')):
    with self.assertRaises(b.Failure):b.create(True)
   self.assertIn(('start','pulse-api','pulse-worker','forum'),calls)
   self.assertFalse(list(root.rglob('manifest.json')))
 def test_retention_protects_failed_pinned_unknown_and_corrupt_sets(self):
  with tempfile.TemporaryDirectory() as tmp:
   parent=Path(tmp)
   def make(day,completed=True):
    root=parent/('202610%02dT000000Z-12345678'%day);root.mkdir();self.fixture(root)
    manifest=json.loads((root/'manifest.json').read_text());manifest['completed']=completed
    (root/'manifest.json').write_text(json.dumps(manifest));return root
   old=make(1);failed=make(2,False);pinned=make(3);(pinned/'.keep').touch()
   corrupt=make(4);(corrupt/'pulse.sql').write_text('corrupt')
   recent=make(5);current=make(6)
   unknown=parent/'legacy';unknown.mkdir()
   b.prune_local(current,2)
   self.assertFalse(old.exists())
   for path in (failed,pinned,corrupt,recent,current,unknown):self.assertTrue(path.exists())
 def test_online_backup_not_reported_as_consistent_restore(self):
  with patch.object(b,'verify',return_value={'consistency':'online-per-database'}):
   with self.assertRaises(b.Failure):b.drill(Path('/unused'),'mysql:8.4')

class ComposeTests(unittest.TestCase):
 @unittest.skipUnless(os.environ.get('METAR_TEST_COMPOSE')=='1','set METAR_TEST_COMPOSE=1 for real Compose rendering')
 def test_all_eight_modes_preserve_volume_and_role_boundaries(self):
  env={k:v for k,v in os.environ.items() if not k.startswith(('PULSE_','FORUM_','NEWAPI_','COMPOSE_','META_PULSE_'))}
  with tempfile.TemporaryDirectory() as tmp:
   path=Path(tmp)/'test.env'
   for pulse,forum,redis in itertools.product(('local','external'),repeat=3):
    path.write_text(('PULSE_DB_PASSWORD=test\nPULSE_DB_ROOT_PASSWORD=test-root\n' if pulse=='local' else '')+('FORUM_DB_PASSWORD=test\nFORUM_DB_ROOT_PASSWORD=test-root\n' if forum=='local' else '')+f'PULSE_DB_MODE={pulse}\nFORUM_DB_MODE={forum}\nPULSE_REDIS_MODE={redis}\n'+('PULSE_DB_DSN=u:p@tcp(external-pulse:3306)/pulse\n' if pulse=='external' else '')+('FORUM_BINDING_GUARD_DSN=u:p@tcp(external-forum:3306)/forum\n' if forum=='external' else '')+('PULSE_REDIS_URL=rediss://u:p@external-redis:6380/0\n' if redis=='external' else ''))
    args=['bash',str(ROOT/'deploy/compose.sh'),'config','--format','json']
    r=subprocess.run(args,env={**env,'META_PULSE_ENV_FILE':str(path),'META_PULSE_COMPOSE_OVERRIDE_FILE':str(Path(tmp)/'absent')},capture_output=True,text=True)
    self.assertEqual(r.returncode,0,r.stderr)
    model=json.loads(r.stdout);services=model['services']
    for service,mode in [('mysql',pulse),('forum-mysql',forum),('redis',redis)]:self.assertEqual(service in services,mode=='local')
    self.assertNotIn('database-client',services)
    for role in ('api','worker'):
     mounts=services['pulse-'+role]['volumes']
     self.assertIn('pulse_'+role+'_runtime_keys',[m.get('source') for m in mounts])
     self.assertNotIn('FORUM_BINDING_GUARD_DSN',services['pulse-'+role]['environment'])
    self.assertNotIn('NEWAPI_LOG_DSN',services['pulse-api']['environment'])

if __name__=='__main__':unittest.main()
