#!/usr/bin/env python3
"""Disposable Docker backup/restore and encrypted repository round-trip."""
import importlib.util
import json
import os
from pathlib import Path
import secrets
import shutil
import subprocess
import tempfile
import time
import unittest
from unittest.mock import patch
ROOT=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('backup',ROOT/'deploy/backup.py')
b=importlib.util.module_from_spec(spec);spec.loader.exec_module(b)

@unittest.skipUnless(os.environ.get('METAR_BACKUP_INTEGRATION')=='1','requires explicit disposable Docker integration opt-in')
class RecoveryRoundTrip(unittest.TestCase):
 def test_real_dump_pause_restore_and_encrypted_readback(self):
  with tempfile.TemporaryDirectory(prefix='metar-recovery-test-') as tmp:
   temp=Path(tmp);project='metar-test-'+secrets.token_hex(5)
   env=temp/'fixture.env';override=temp/'override.yml'
   env.write_text('COMPOSE_PROJECT_NAME='+project+'\nPULSE_DB_PASSWORD=test\nPULSE_DB_ROOT_PASSWORD=test-root\nFORUM_DB_PASSWORD=test\nFORUM_DB_ROOT_PASSWORD=test-root\nMETA_PULSE_MYSQL_IMAGE='+os.environ.get('METAR_TEST_MYSQL_IMAGE','mysql:8.0')+'\n')
   override.write_text('services:\n'+''.join('  '+s+':\n    build: !reset null\n    image: alpine:3.20\n    entrypoint: [sh, -c, "sleep 3600"]\n    healthcheck:\n      disable: true\n' for s in ('pulse-api','pulse-worker','forum')))
   test_env={**os.environ,'META_PULSE_ENV_FILE':str(env),'META_PULSE_COMPOSE_OVERRIDE_FILE':str(override)}
   def cp(*args):
    r=subprocess.run(['bash',str(ROOT/'deploy/compose.sh'),*args],env=test_env,capture_output=True,text=True)
    if r.returncode:raise AssertionError('compose '+str(args[:2])+' failed: '+r.stderr[-1500:])
    return r.stdout.strip()
   subprocess.run(['git','init','-q',str(temp)],check=True)
   subprocess.run(['git','-C',str(temp),'-c','user.name=Test','-c','user.email=test@example.invalid','-c','commit.gpgsign=false','commit','--allow-empty','-qm','fixture'],check=True)
   try:
    cp('up','-d','--no-build','pulse-api','pulse-worker','forum')
    for role in ('api','worker'):cp('exec','-T','pulse-'+role,'sh','-c','printf test-private-key > /app/runtime-keys/role.key')
    cp('exec','-T','forum','sh','-c','mkdir -p /data/conf/conf /data/conf/uploads; printf "site: retained\n" > /data/conf/conf/config.yaml; printf test-attachment > /data/conf/uploads/avatar.png')
    def sql(service,query):
     return cp('exec','-T',service,'sh','-c','MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot "$MYSQL_DATABASE" -e "$1"','sh',query)
    sql('mysql',"CREATE TABLE pulse_account (id BIGINT PRIMARY KEY,user_id BIGINT,period_id BIGINT,asset_type VARCHAR(20),balance BIGINT,version BIGINT); CREATE TABLE pulse_ledger_entry (id BIGINT PRIMARY KEY,user_id BIGINT,period_id BIGINT,asset_type VARCHAR(20),amount BIGINT); INSERT INTO pulse_account VALUES(1,1,1,'ticket',5,1); INSERT INTO pulse_ledger_entry VALUES(1,1,1,'ticket',5); CREATE TRIGGER trg_pulse_ledger_entry_no_update BEFORE UPDATE ON pulse_ledger_entry FOR EACH ROW SIGNAL SQLSTATE '45000' SET MESSAGE_TEXT='append only';")
    sql('forum-mysql',"CREATE TABLE metar_exp_account (user_id BIGINT PRIMARY KEY,balance BIGINT); CREATE TABLE metar_exp_ledger (user_id BIGINT,delta BIGINT); INSERT INTO metar_exp_account VALUES(1,10); INSERT INTO metar_exp_ledger VALUES(1,10);")
    with patch.dict(os.environ,test_env),patch.object(b,'ROOT',temp):
     with b.lock():snapshot=b.create(True)
     self.assertEqual(b.verify(snapshot)['consistency'],'quiesced')
     self.assertEqual(set(cp('ps','--status','running','--services').splitlines()),{'mysql','forum-mysql','redis','pulse-api','pulse-worker','forum'})
     b.drill(snapshot,'mysql:8.4')
     # Existing isolated DBs now stand in for separately managed endpoints.
     # No local root secrets in deployment settings; backup uses explicit users.
     for service,db in [('mysql','meta_pulse'),('forum-mysql','meta_pulse_forum')]:
      sql(service,"CREATE USER 'backup'@'%' IDENTIFIED BY 'test-backup'; GRANT SELECT, SHOW VIEW, TRIGGER, EVENT ON "+db+".* TO 'backup'@'%'; GRANT SHOW_ROUTINE ON *.* TO 'backup'@'%';")
     original_env=env.read_text()
     try:
      env.write_text('COMPOSE_PROJECT_NAME='+project+'\nPULSE_DB_MODE=external\nFORUM_DB_MODE=external\nPULSE_REDIS_MODE=external\nPULSE_REDIS_URL=redis://redis:6379/0\nPULSE_DB_DSN=pulse:test@tcp(mysql:3306)/meta_pulse\nFORUM_BINDING_GUARD_DSN=forum:test@tcp(forum-mysql:3306)/meta_pulse_forum\nPULSE_BACKUP_DB_DSN=backup:test-backup@tcp(mysql:3306)/meta_pulse\nFORUM_BACKUP_DB_DSN=backup:test-backup@tcp(forum-mysql:3306)/meta_pulse_forum\n')
      external=b.create(True)
      b.drill(external,'mysql:8.4')
      env.write_text(env.read_text().replace('backup:test-backup@','backup:wrong-password@'))
      failed=temp/'failed.sql'
      with self.assertRaises(b.Failure):b.dump('pulse',failed)
      self.assertFalse(failed.exists())
     finally:env.write_text(original_env)

     if shutil.which('restic'):
      password=temp/'restic-password';password.write_text('public-isolated-test-password');password.chmod(0o600)
      cfg=temp/'offsite.env';cfg.write_text('RESTIC_REPOSITORY='+str(temp/'repository')+'\nRESTIC_PASSWORD_FILE='+str(password)+'\n');cfg.chmod(0o600)
      subprocess.run(['restic','init'],env={**os.environ,**b.envfile(cfg)},check=True,stdout=subprocess.DEVNULL)
      self.assertTrue(b.offsite(snapshot,cfg))
     else:self.fail('restic required for encrypted round-trip')
   finally:cp('down','-v','--remove-orphans')

if __name__=='__main__':unittest.main()
