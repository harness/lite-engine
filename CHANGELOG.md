# Changelog

## [v0.2.0](https://github.com/harness/lite-engine/tree/v0.2.0) (2022-05-27)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.1.0...v0.2.0)

**Implemented enhancements:**

- \(feat\) provides the ability to disable the auto mount of the docker socket [\#63](https://github.com/harness/lite-engine/pull/63) ([eoinmcafee00](https://github.com/eoinmcafee00))

**Fixed bugs:**

- \(fix\) update yaml.v3 [\#64](https://github.com/harness/lite-engine/pull/64) ([tphoney](https://github.com/tphoney))

**Merged pull requests:**

- use the anka runner [\#62](https://github.com/harness/lite-engine/pull/62) ([tphoney](https://github.com/tphoney))

## [v0.1.0](https://github.com/harness/lite-engine/tree/v0.1.0) (2022-05-05)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.2...v0.1.0)

**Implemented enhancements:**

- \(feat\) add version to health check, and set it in cli [\#58](https://github.com/harness/lite-engine/pull/58) ([tphoney](https://github.com/tphoney))
- \(feat\) re-add acceptance tests, lite-engine can run in HTTP mode [\#56](https://github.com/harness/lite-engine/pull/56) ([tphoney](https://github.com/tphoney))

**Merged pull requests:**

- update drone yml to use type of vm for osx [\#61](https://github.com/harness/lite-engine/pull/61) ([tphoney](https://github.com/tphoney))
- \(maint\) release prep for v0.1.0 [\#60](https://github.com/harness/lite-engine/pull/60) ([tphoney](https://github.com/tphoney))
- \(maint\) change build type to vm in drone.yml [\#59](https://github.com/harness/lite-engine/pull/59) ([tphoney](https://github.com/tphoney))
- Osx acceptance testing [\#57](https://github.com/harness/lite-engine/pull/57) ([tphoney](https://github.com/tphoney))

## [v0.0.2](https://github.com/harness/lite-engine/tree/v0.0.2) (2022-04-14)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.15...v0.0.2)

**Implemented enhancements:**

- append usr/local/bin path for osx docker [\#53](https://github.com/harness/lite-engine/pull/53) ([eoinmcafee00](https://github.com/eoinmcafee00))

**Fixed bugs:**

- Security fixes [\#52](https://github.com/harness/lite-engine/pull/52) ([shubham149](https://github.com/shubham149))
- Fix output variable support for pwsh [\#51](https://github.com/harness/lite-engine/pull/51) ([shubham149](https://github.com/shubham149))

**Merged pull requests:**

- release prep v0.0.2 [\#55](https://github.com/harness/lite-engine/pull/55) ([eoinmcafee00](https://github.com/eoinmcafee00))
- Add license to the repository [\#50](https://github.com/harness/lite-engine/pull/50) ([shubham149](https://github.com/shubham149))

## [v0.0.1.15](https://github.com/harness/lite-engine/tree/v0.0.1.15) (2022-02-24)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.14...v0.0.1.15)

**Implemented enhancements:**

- Adding step logs for image pull errors [\#49](https://github.com/harness/lite-engine/pull/49) ([shubham149](https://github.com/shubham149))
- Moved docker/image package from internal to engine [\#48](https://github.com/harness/lite-engine/pull/48) ([marko-gacesa](https://github.com/marko-gacesa))

## [v0.0.1.14](https://github.com/harness/lite-engine/tree/v0.0.1.14) (2022-02-09)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.13...v0.0.1.14)

**Implemented enhancements:**

- Bind container port to host port [\#46](https://github.com/harness/lite-engine/pull/46) ([shubham149](https://github.com/shubham149))

## [v0.0.1.13](https://github.com/harness/lite-engine/tree/v0.0.1.13) (2022-02-01)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.12...v0.0.1.13)

**Merged pull requests:**

- \(fix\) check before prepending a drive letter on windows [\#45](https://github.com/harness/lite-engine/pull/45) ([tphoney](https://github.com/tphoney))
- fixed drone log streaming [\#44](https://github.com/harness/lite-engine/pull/44) ([marko-gacesa](https://github.com/marko-gacesa))
- extend os support [\#43](https://github.com/harness/lite-engine/pull/43) ([eoinmcafee00](https://github.com/eoinmcafee00))
- drone support: step exec and output steaming [\#42](https://github.com/harness/lite-engine/pull/42) ([marko-gacesa](https://github.com/marko-gacesa))

## [v0.0.1.12](https://github.com/harness/lite-engine/tree/v0.0.1.12) (2022-01-05)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.11...v0.0.1.12)

**Merged pull requests:**

- add account ID in download link call [\#41](https://github.com/harness/lite-engine/pull/41) ([vistaarjuneja](https://github.com/vistaarjuneja))

## [v0.0.1.11](https://github.com/harness/lite-engine/tree/v0.0.1.11) (2021-12-27)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.10...v0.0.1.11)

**Merged pull requests:**

- Add e2e support for dotnet and nunit console [\#36](https://github.com/harness/lite-engine/pull/36) ([vistaarjuneja](https://github.com/vistaarjuneja))

## [v0.0.1.10](https://github.com/harness/lite-engine/tree/v0.0.1.10) (2021-12-23)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.9...v0.0.1.10)

**Merged pull requests:**

- Set parent process env in steps running on host VM [\#39](https://github.com/harness/lite-engine/pull/39) ([shubham149](https://github.com/shubham149))

## [v0.0.1.9](https://github.com/harness/lite-engine/tree/v0.0.1.9) (2021-12-23)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.8...v0.0.1.9)

**Merged pull requests:**

- Fix output variable support for windows [\#38](https://github.com/harness/lite-engine/pull/38) ([shubham149](https://github.com/shubham149))

## [v0.0.1.8](https://github.com/harness/lite-engine/tree/v0.0.1.8) (2021-12-22)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.7...v0.0.1.8)

**Merged pull requests:**

- \(feat\) implement file/folder creation on setup [\#37](https://github.com/harness/lite-engine/pull/37) ([tphoney](https://github.com/tphoney))
- Add support for output vars for windows and proxy env for ti & log svc call [\#35](https://github.com/harness/lite-engine/pull/35) ([shubham149](https://github.com/shubham149))

## [v0.0.1.7](https://github.com/harness/lite-engine/tree/v0.0.1.7) (2021-12-17)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.6...v0.0.1.7)

**Merged pull requests:**

- Log retries on health check [\#34](https://github.com/harness/lite-engine/pull/34) ([shubham149](https://github.com/shubham149))
- Mount docker socket to all the containers [\#33](https://github.com/harness/lite-engine/pull/33) ([shubham149](https://github.com/shubham149))
- Add test intelligence step support [\#32](https://github.com/harness/lite-engine/pull/32) ([vistaarjuneja](https://github.com/vistaarjuneja))

## [v0.0.1.6](https://github.com/harness/lite-engine/tree/v0.0.1.6) (2021-12-10)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1.5...v0.0.1.6)

**Merged pull requests:**

- Fix panic in lite-engine [\#31](https://github.com/harness/lite-engine/pull/31) ([shubham149](https://github.com/shubham149))
- \(maint\) release automation [\#30](https://github.com/harness/lite-engine/pull/30) ([tphoney](https://github.com/tphoney))

## [v0.0.1.5](https://github.com/harness/lite-engine/tree/v0.0.1.5) (2021-12-08)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.1...v0.0.1.5)

**Implemented enhancements:**

- Retry\* client calls have timeout as a parameter [\#29](https://github.com/harness/lite-engine/pull/29) ([marko-gacesa](https://github.com/marko-gacesa))
- remove attributes from setup response [\#26](https://github.com/harness/lite-engine/pull/26) ([eoinmcafee00](https://github.com/eoinmcafee00))
- update poll step timeout to 4 hours [\#25](https://github.com/harness/lite-engine/pull/25) ([eoinmcafee00](https://github.com/eoinmcafee00))

**Fixed bugs:**

- github token name missed from drone.yml [\#28](https://github.com/harness/lite-engine/pull/28) ([eoinmcafee00](https://github.com/eoinmcafee00))

**Merged pull requests:**

- Fix api parameters for setup call [\#27](https://github.com/harness/lite-engine/pull/27) ([shubham149](https://github.com/shubham149))

## [v0.0.1](https://github.com/harness/lite-engine/tree/v0.0.1) (2021-12-06)

[Full Changelog](https://github.com/harness/lite-engine/compare/v0.0.0...v0.0.1)

**Fixed bugs:**

- add depends on to release step [\#22](https://github.com/harness/lite-engine/pull/22) ([eoinmcafee00](https://github.com/eoinmcafee00))

**Merged pull requests:**

- release prep for v0.0.1 [\#24](https://github.com/harness/lite-engine/pull/24) ([eoinmcafee00](https://github.com/eoinmcafee00))

## [v0.0.0](https://github.com/harness/lite-engine/tree/v0.0.0) (2021-12-03)

[Full Changelog](https://github.com/harness/lite-engine/compare/5f26deba117780467848b2cecf738e7428e41d4a...v0.0.0)

**Implemented enhancements:**

- add steps for building windows & linux - push binaries to github [\#21](https://github.com/harness/lite-engine/pull/21) ([eoinmcafee00](https://github.com/eoinmcafee00))
- update setup to match api documentation: [\#17](https://github.com/harness/lite-engine/pull/17) ([eoinmcafee00](https://github.com/eoinmcafee00))
- certs are now passed in already read, not file location [\#15](https://github.com/harness/lite-engine/pull/15) ([eoinmcafee00](https://github.com/eoinmcafee00))
- \(feat\) add RetryPollStep and RetryHealth [\#14](https://github.com/harness/lite-engine/pull/14) ([tphoney](https://github.com/tphoney))
- Add support for run test step [\#11](https://github.com/harness/lite-engine/pull/11) ([shubham149](https://github.com/shubham149))
- \(feat\) improve healthz handler and add initial setup [\#10](https://github.com/harness/lite-engine/pull/10) ([tphoney](https://github.com/tphoney))
- Add support for output variables [\#9](https://github.com/harness/lite-engine/pull/9) ([shubham149](https://github.com/shubham149))
- Added support for test report upload [\#7](https://github.com/harness/lite-engine/pull/7) ([shubham149](https://github.com/shubham149))
- Add remote log stream support [\#5](https://github.com/harness/lite-engine/pull/5) ([shubham149](https://github.com/shubham149))
- Add functionality for executing steps [\#3](https://github.com/harness/lite-engine/pull/3) ([shubham149](https://github.com/shubham149))

**Fixed bugs:**

- update setup response [\#19](https://github.com/harness/lite-engine/pull/19) ([eoinmcafee00](https://github.com/eoinmcafee00))
- Update formatting of step executor [\#8](https://github.com/harness/lite-engine/pull/8) ([shubham149](https://github.com/shubham149))

**Merged pull requests:**

- release prep v0.0.0 [\#20](https://github.com/harness/lite-engine/pull/20) ([eoinmcafee00](https://github.com/eoinmcafee00))
- Add testing on ubuntu [\#18](https://github.com/harness/lite-engine/pull/18) ([tphoney](https://github.com/tphoney))
- Move from cloud.drone.io to harness.drone.io [\#16](https://github.com/harness/lite-engine/pull/16) ([tphoney](https://github.com/tphoney))
- \(maint\) cleanup of setup code [\#12](https://github.com/harness/lite-engine/pull/12) ([tphoney](https://github.com/tphoney))
- \(maint\) use a proper docker version [\#6](https://github.com/harness/lite-engine/pull/6) ([tphoney](https://github.com/tphoney))
- \(maint\) move to the harness namespace [\#4](https://github.com/harness/lite-engine/pull/4) ([tphoney](https://github.com/tphoney))
- \(maint\) adding project basics [\#1](https://github.com/harness/lite-engine/pull/1) ([tphoney](https://github.com/tphoney))



\* *This Changelog was automatically generated by [github_changelog_generator](https://github.com/github-changelog-generator/github-changelog-generator)*
