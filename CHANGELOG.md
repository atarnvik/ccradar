# Changelog

## [0.8.0](https://github.com/atarnvik/ccradar/compare/v0.7.0...v0.8.0) (2026-10-27)


### Features

* find sessions by description, recent-first sort, current transcript format fixes ([#6](https://github.com/atarnvik/ccradar/issues/6)) ([e03ca72](https://github.com/atarnvik/ccradar/commit/e03ca72955c817e7be5fdb0075d05fcbae182824))

## [0.7.0](https://github.com/atarnvik/ccradar/compare/v0.6.0...v0.7.0) (2026-07-23)


### Features

* read container history and group both views by source ([b9f3375](https://github.com/atarnvik/ccradar/commit/b9f33755f1885d24e173f267e6e7375115642dd9))
* read sessions from Docker containers (--docker) ([e3d4ed1](https://github.com/atarnvik/ccradar/commit/e3d4ed109ca07242bd3b5b10c5982c2e49d3e1fe))


### Bug Fixes

* resume container sessions as the owning user with HOME ([38d647b](https://github.com/atarnvik/ccradar/commit/38d647b6048af51f2e549bfd380e5a172e88f888))
* resume container sessions via an interactive login shell ([31fa31c](https://github.com/atarnvik/ccradar/commit/31fa31c3d614f8a9cca94a9e20691e4078f57ad2))

## [0.6.0](https://github.com/atarnvik/ccradar/compare/v0.5.0...v0.6.0) (2026-06-16)


### Features

* add a preview pane for the selected session ([11ef768](https://github.com/atarnvik/ccradar/commit/11ef7684fff02e618a9cc1ca8d96dd4c45e5b9ba))
* click a notification to jump to its session tab ([1e2d5e7](https://github.com/atarnvik/ccradar/commit/1e2d5e7ff459910df93fa27f14ff92ce024a1ae9))


### Bug Fixes

* default the preview pane to off (opt-in via p) ([206ce5e](https://github.com/atarnvik/ccradar/commit/206ce5efa7308e26aedc134061bcf902758276b7))

## [0.5.0](https://github.com/atarnvik/ccradar/compare/v0.4.1...v0.5.0) (2026-06-16)


### Features

* also notify when a session needs input (busy→waiting) ([7dfb8fb](https://github.com/atarnvik/ccradar/commit/7dfb8fba345919ea49d74ca2e3e25792bfa0cda3))


### Bug Fixes

* detect new releases via GitHub instead of the Go proxy ([d0ce0d0](https://github.com/atarnvik/ccradar/commit/d0ce0d019e947fdd5aaf0020feb5be0f57d9bd6c))
* load history at startup so the Historical count is accurate ([d86186e](https://github.com/atarnvik/ccradar/commit/d86186eff68c745caa9e94596897a69a61f6f440))
* match brand-new (untitled) sessions to their tab ([a5458ec](https://github.com/atarnvik/ccradar/commit/a5458ec3504449cf10e72909683dea679623019f))


### Documentation

* add demo GIF to the README ([f359fb2](https://github.com/atarnvik/ccradar/commit/f359fb2d619b6dda2ee92a9b6a042fc8a72535bc))

## [0.4.1](https://github.com/atarnvik/ccradar/compare/v0.4.0...v0.4.1) (2026-06-16)


### Bug Fixes

* show the right upgrade command for the install method ([4cdc479](https://github.com/atarnvik/ccradar/commit/4cdc47954da68a02a1f37fc52b2d5503fbd43a00))

## [0.4.0](https://github.com/atarnvik/ccradar/compare/v0.3.0...v0.4.0) (2026-06-15)


### Features

* add --version flag ([2ef931c](https://github.com/atarnvik/ccradar/commit/2ef931cec9fb188920fa926cced6882150751c9a))
