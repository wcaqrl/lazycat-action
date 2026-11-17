# Image copy progress and AFFiNE Docker compatibility

The user approved fixing both repositories, testing, publishing a patch release
and exercising AFFiNE's real packaging/submission flow after verification.

AFFiNE's `verify_local` normalizes only missing/null User and WorkingDir to empty
strings. All nonempty values, types, other runtime settings, digests, platforms
and filesystem layers retain strict checks. Characterize Docker API 1.48/1.54
against the actual pinned images without modifying the daemon.

Lazycat-action owns copy monitoring in the existing delivery resolver so both
source-pipeline multi-image and existing image/prepared-image flows are covered.
Log image ID, source, platform, elapsed time, completed/total layers and individual
layer percentages. Report waiting for finalization when every known layer is at
100% but finished=false. Emit a heartbeat at most every 15s plus phase changes,
and only report completion after validating finished=true and an official result.

Add optional per-image delivery.copy_timeout and delivery.copy_idle_timeout Go
duration strings. Zero/omitted use 1h total and min(20m,total) idle. Negative values
or an explicit idle above the total are invalid; options require lazycat delivery.
AFFiNE opts into 30m total/10m idle for all three images. Timeouts concern copying,
not container acceptance, and do not affect artifact/source fingerprint identity.
Unchanged polls, reordered layers and repeated 100% snapshots do not reset idle;
new layers or increasing high-water progress do. Finalization is bounded too.
Cancellation stops the in-flight HTTP request and monitor; errors include image,
kind, elapsed and last progress. No automatic task resubmission or retry is added.

Approach: normalize semantic empty scalar fields and monitor the existing copier.
Skipping runtime verification or changing the Runner's Docker version would hide
the defect; copying/packaging safety checks remain in place. Existing GitHub token
isolation, review/state handling, source selection and other adapter config stay.

Validate locally, obtain one fresh-context code review, then publish v1.10.1 after
CI. Verify the fixed adapter in a hosted Runner before the real submission. No
large local Docker tests; use existing local images for lightweight API checks.
