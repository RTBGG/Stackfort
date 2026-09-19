# Beta.10 candidate qualification — 2026-09-19

Status: **source prepared; no completed candidate build or fresh-host qualification
claimed. Not qualified for publication.**

This candidate retains the native installation and NGINX activation fixes, and
corrects the staged-file ACL defect found by the
[beta.9 installed product smoke](2026-09-19-beta9-candidate-qualification.md).
Uploaded files now receive the destination's default ACL before publication.
Copy/archive staging remains private while descendants inherit the destination
policy. Backup restoration retains live directory policies, including private
directories and nested web roots. Normal move/trash inode permissions are unchanged.

Five focused Linux tests and five negative subcases passed unprivileged and as
root. The root pass executes real UID/GID 65534 reads, verifying web readability
and private/staging denial. The complete Linux hostfiles suite and Linux vet also
passed. CI now includes the privileged ACL regression pass. These local results
are source tests, not qualification of a release archive.

Required next: exact-source CI/security, an original manual build, mechanical
candidate selection, genuine tag provenance, and fresh full onboarding/product
smoke. Rerun/reboot, host/security/isolation/OCI/failure/removal tests, actual
candidate-specific publication approval and publication gates remain mandatory.
No beta.9 tag, installed binary, credential, ACL or failed fixture is repaired or
reused as successful qualification. The reserved final-removal disk pair remains
unconsumed.
