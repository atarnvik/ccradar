# Changelog

## [0.11.0](https://github.com/atarnvik/ccradar/compare/v0.10.0...v0.11.0) (2026-11-09)


### Features

* cleaner preview pane with ruled section headings ([#24](https://github.com/atarnvik/ccradar/issues/24)) ([737851d](https://github.com/atarnvik/ccradar/commit/737851dc4f9882253609e745cd7cfa11fbbd359c))
* look PRs up with every logged-in gh account ([#25](https://github.com/atarnvik/ccradar/issues/25)) ([e02093f](https://github.com/atarnvik/ccradar/commit/e02093f8242b3e452f392c0d6fc774b1b583d347))
* mark merged and closed PRs with ⑂ and × ([#26](https://github.com/atarnvik/ccradar/issues/26)) ([179a1c3](https://github.com/atarnvik/ccradar/commit/179a1c34a6e0be779d935f20cd59f03ec8f52651))
* o opens a session's open PRs; O picks from all of them ([#29](https://github.com/atarnvik/ccradar/issues/29)) ([2ca2a91](https://github.com/atarnvik/ccradar/commit/2ca2a914422325c234cb656039d8a45a6a7dbc4f))
* o opens all of a session's PRs; badge counts the others ([#27](https://github.com/atarnvik/ccradar/issues/27)) ([d014302](https://github.com/atarnvik/ccradar/commit/d01430216c27e9dc1e78eb31ae6311fcf4cbc61d))
* PR badges with live status, and o to open a session's PR ([#16](https://github.com/atarnvik/ccradar/issues/16)) ([dd3cc9f](https://github.com/atarnvik/ccradar/commit/dd3cc9fd3f37a3d4557aa6baebf1051dfc593200))
* resize the preview pane with [ and ] ([#23](https://github.com/atarnvik/ccradar/issues/23)) ([5f874b7](https://github.com/atarnvik/ccradar/commit/5f874b7df1ed752791f3e1062ddc518de37bb3fd))
* show background shells a live session left running ([#17](https://github.com/atarnvik/ccradar/issues/17)) ([4c52d85](https://github.com/atarnvik/ccradar/commit/4c52d85eb722abc6654fc190e72f7df9fc2d373f))
* show recaps, custom session names, and (on $) past-session cost ([#15](https://github.com/atarnvik/ccradar/issues/15)) ([b1e8f9b](https://github.com/atarnvik/ccradar/commit/b1e8f9bb8e892f6a779143a51198398bdb281b3b))


### Bug Fixes

* age live sessions by statusUpdatedAt ([#20](https://github.com/atarnvik/ccradar/issues/20)) ([1c11084](https://github.com/atarnvik/ccradar/commit/1c1108494199a942fbae28cb95401e40ef6ca530))
* don't panic when the terminal reports zero width ([#14](https://github.com/atarnvik/ccradar/issues/14)) ([39c268f](https://github.com/atarnvik/ccradar/commit/39c268fdb21222be93d1ad344fa6fecf4584a5d0))


### Performance

* cache transcript scans and PR statuses on disk ([#30](https://github.com/atarnvik/ccradar/issues/30)) ([1ad13ad](https://github.com/atarnvik/ccradar/commit/1ad13add7b894431e58025b4804769340574c5fe))
* cache transcript scans by size and mtime ([#21](https://github.com/atarnvik/ccradar/issues/21)) ([d009121](https://github.com/atarnvik/ccradar/commit/d009121b6893437294a1c92e121e7acdead41fbe))


### Refactors

* move sources to cmd/ccradar and split main.go ([#12](https://github.com/atarnvik/ccradar/issues/12)) ([b58ce8d](https://github.com/atarnvik/ccradar/commit/b58ce8d853e44032eb70ec4c295a5c6a7acf1f6c))

## [0.10.0](https://github.com/atarnvik/ccradar/compare/v0.9.0...v0.10.0) (2026-11-08)


### Features

* --popup mode that quits after jumping to a session ([#10](https://github.com/atarnvik/ccradar/issues/10)) ([95b7c6a](https://github.com/atarnvik/ccradar/commit/95b7c6ac2869e2cda7adaea0139916aed0cf0261))

## [0.9.0](https://github.com/atarnvik/ccradar/compare/v0.8.0...v0.9.0) (2026-11-08)


### Features

* support Rex terminal ([#8](https://github.com/atarnvik/ccradar/issues/8)) ([ec8df04](https://github.com/atarnvik/ccradar/commit/ec8df041f2c4323693b1d148f74186a160ee077d))

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
