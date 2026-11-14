#!/bin/sh
# Standalone computation evidence only; this does not certify the platform runner.
set -eu
cd "$(dirname "$0")/.."
fixture_root=$(mktemp -d /tmp/eduforge-centroid.XXXXXX)
cp testdata/centroid/experiment.mjs "$fixture_root/experiment.mjs"
git -C "$fixture_root" init --quiet -b main
git -C "$fixture_root" config user.name EduForge
git -C "$fixture_root" config user.email eduforge@localhost
git -C "$fixture_root" add experiment.mjs
git -C "$fixture_root" commit --quiet -m 'Synthetic nearest-centroid acceptance fixture'
git -C "$fixture_root" checkout --quiet -b eduforge/standalone-fixture
git -C "$fixture_root" rev-parse HEAD > "$fixture_root/source-commit.txt"
cd "$fixture_root"
node experiment.mjs > execution.log 2>&1
node --input-type=module <<'JS'
import {readFileSync,writeFileSync} from 'node:fs'
import assert from 'node:assert/strict'
const row=readFileSync('results.csv','utf8').trim().split('\n')[1].split(',').map(Number)
assert.deepEqual(row,[1,4,4])
const log=JSON.parse(readFileSync('execution.log','utf8'))
assert.equal(log.predictions.length,4)
assert.equal(log.accuracy,row[0])
writeFileSync('verification.json',JSON.stringify({scope:'standalone synthetic computation; platform runner not verified',command:['node','experiment.mjs'],source_commit:readFileSync('source-commit.txt','utf8').trim(),execution:'completed',comparison:'matched',criterion:{metric:'accuracy',expected:1,tolerance:0},actual:row[0],node:process.version,at:new Date().toISOString()},null,2)+'\n')
JS
printf '%s\n' "$fixture_root"
