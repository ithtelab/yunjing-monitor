# Open-source readiness

Status: **ready for public source release**.

Completed checks:

- [x] production credentials and runtime data are excluded by `.gitignore`;
- [x] automated dependency, vulnerability, build, and secret checks are configured;
- [x] security, contribution, support, conduct, and third-party notices are present;
- [x] independently maintained frontend components replaced the identified legacy implementations;
- [x] the project is distributed under the MIT License;
- [x] the public repository starts from a clean root commit without private development history.

Repository launch tasks:

- [ ] Verify all GitHub Actions checks on the public repository.
- [ ] Enable private vulnerability reporting, branch protection, and dependency alerts.
- [ ] Configure `RELEASE_SIGNING_KEY` before publishing signed release assets.
- [ ] Verify a clean installation and rollback from the first public release.
