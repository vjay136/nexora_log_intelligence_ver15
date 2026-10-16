# NLI V13 support matrix

This matrix distinguishes release-tested support from formats that have code or configuration present but still need deployment acceptance. It records the evidence currently checked into this repository; it is not a promise that an untested environment will work.

| Area | Supported | Experimental / needs release evidence | Unsupported / not claimed |
|---|---|---|---|
| Linux packages | Rocky Linux 9 and 10, x86_64 RPMs with systemd. The Rocky acceptance scripts exercise package install, services, API, embedded WebView, and lifecycle. The additive AWS wrappers recognize x86_64 RHEL/Rocky 8, 9, and 10 and fail closed on other hosts. | RHEL 8/9/10 and Rocky 8 runtime acceptance on the user's exact AWS image, repositories, SELinux policy, VPC and security groups. The preflight contract is tested, but only the user's instance can establish that complete infrastructure path. | Debian/Ubuntu packages, DEB, ARM/aarch64 RPMs, and a generic “all Linux” claim. |
| Install and upgrade | Guided shell installers plus additive noninteractive AWS controller/client wrappers. V8-to-V13 migration is a release gate only when genuine V8 client and controller RPMs are supplied. | Additional configuration-management integrations beyond the supplied wrappers. | A universal installer that chooses packages across unrelated OS families. |
| Service runtime | systemd units packaged in RPMs; service validation is part of the Rocky matrix. | Other init systems or service managers. | Claiming systemd services run unchanged on non-systemd systems. |
| Controller WebView | WebView embedded and served by the controller RPM, sharing the controller's authenticated API. | — | Frontend files served alone as a working control plane; static files do not implement the login/API contract. |
| Hosted webapp | Vercel and Cloudflare Pages build/proxy configurations are present; origin validation, authentication forwarding, SSE, and API proxy contract checks are present. A public HTTPS Cloudflare controller hostname is required. | Live Vercel/Pages acceptance remains environment-specific until the operator supplies the DNS zone, named tunnel, and hosted-project access. | Arbitrary static-only hosting for authenticated operation, or use of Supabase/private controller addresses as the public controller origin. |
| Web package formats | `webview_ver13.zip` and `webapp_ver13.zip` are checked by the release verifier; RPMs contain the controller WebView. | Generic hosting bundles and OCI images. | Claiming DEB/MSI/appliance/OCI releases before artifacts and target acceptance exist. |
| Architecture | Linux x86_64 for current RPMs. | Additional architectures after native or cross-build, dependency, and target-runtime acceptance. | Universal CPU/OS portability based only on Go source portability. |

## Evidence needed to expand support

Add an OS or package format only after fresh install, upgrade, rollback/reinstall, service health, permissions, SELinux/firewall behavior where relevant, and uninstall/state-preservation checks pass on that target. Add a web host only after its actual deployment artifact is built and the authenticated browser-to-API path passes, including cookies, CSRF, request methods, streaming/WebSocket needs, and secret isolation. Keep the evidence tied to exact OS image digests, architectures, package hashes, and source revision.

The current Rocky matrix names Rocky Linux 9.6 and Rocky Linux 10.2 container images. Results from those images do not establish compatibility with every RHEL derivative or future minor release. The current package specs restrict architecture to x86_64 and require systemd tooling; see `packaging/rpm/` and `packaging/systemd/`.
