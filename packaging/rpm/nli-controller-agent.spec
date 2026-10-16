%{!?nli_version:%global nli_version 13.0.0}
%{!?nli_release:%global nli_release 1}
Name:           nli-controller-agent
Version:        %{nli_version}
Release:        %{nli_release}%{?dist}
Summary:        Nexora Log Inetelligence Ver13 controller and Web console
License:        LicenseRef-Proprietary
URL:            https://github.com/vjay136/nexora_log_intelligence_ver13.git
Source0:        %{name}-%{version}.tar.gz
ExclusiveArch:  x86_64
Requires(pre):  shadow-utils
Requires(post): systemd coreutils sed grep
Requires(preun): systemd
Requires(postun): systemd
Requires:       python3 curl openssl hostname iproute
AutoReqProv:    no
%global debug_package %{nil}
%global _build_id_links none
%global __os_install_post %{nil}

%description
NLI v13 controller providing authenticated enrollment, fleet synchronization,
rollouts, telemetry APIs, and the byte-verified webview_ver13 console.

%prep
%autosetup -n %{name}-%{version}

%build
# The static binary and Web assets are built and verified before rpmbuild.

%install
install -d %{buildroot}%{_bindir} %{buildroot}%{_sbindir} %{buildroot}%{_sysconfdir}/nli
install -d %{buildroot}/usr/lib/systemd/system %{buildroot}%{_datadir}/nli/webview_ver13 %{buildroot}%{_datadir}/nli/supabase
install -d %{buildroot}%{_docdir}/%{name} %{buildroot}/var/lib/nli-controller
install -m 0755 bin/nli-controller-agent bin/nli %{buildroot}%{_bindir}/
install -m 0755 sbin/nli-controller-setup %{buildroot}%{_sbindir}/nli-controller-setup
install -m 0755 sbin/nli-supabase-setup %{buildroot}%{_sbindir}/nli-supabase-setup
install -m 0755 sbin/nli-admin-reset %{buildroot}%{_sbindir}/nli-admin-reset
install -m 0644 supabase/nli_schema.sql %{buildroot}%{_datadir}/nli/supabase/nli_schema.sql
install -m 0640 configs/controller.json configs/controller.env configs/geoip.env %{buildroot}%{_sysconfdir}/nli/
install -m 0644 systemd/nli-controller-agent.service systemd/nli-controller-bootstrap.service systemd/nli-geoip-update.service systemd/nli-geoip-update.timer %{buildroot}/usr/lib/systemd/system/
cp -a web/. %{buildroot}%{_datadir}/nli/webview_ver13/
find %{buildroot}%{_datadir}/nli/webview_ver13 -type d -exec chmod 0755 {} +
find %{buildroot}%{_datadir}/nli/webview_ver13 -type f -exec chmod 0644 {} +
install -m 0644 README.md %{buildroot}%{_docdir}/%{name}/README.md

%pre
getent group nli >/dev/null || groupadd -r nli
getent passwd nli >/dev/null || useradd -r -g nli -d /var/lib/nli-controller -s /sbin/nologin nli

%post
config_file=%{_sysconfdir}/nli/controller.json
if grep -Eq '"web_dir"[[:space:]]*:[[:space:]]*"/usr/share/nli/webview_ver[15678]"' "$config_file"; then
  sed -i -E 's#/usr/share/nli/webview_ver[15678](["/])#/usr/share/nli/webview_ver13\1#g' "$config_file"
fi
secret_file=%{_sysconfdir}/nli/controller.env
chown root:nli "$secret_file"
chmod 0640 "$secret_file"
systemctl daemon-reload >/dev/null 2>&1 || :
systemctl enable nli-controller-bootstrap.service >/dev/null 2>&1 || :
# The GeoIP timer remains disabled until an administrator accepts the upstream
# license in /etc/nli/geoip.env and deliberately enables it.
if [ -s /etc/nli/controller-bootstrap.env ]; then systemctl start nli-controller-bootstrap.service >/dev/null 2>&1 || :; fi

%preun
if [ "$1" -eq 0 ]; then
  systemctl disable --now nli-controller-bootstrap.service nli-controller-agent.service >/dev/null 2>&1 || :
fi

%postun
systemctl daemon-reload >/dev/null 2>&1 || :

%files
%attr(0755,root,root) %{_bindir}/nli-controller-agent
%attr(0755,root,root) %{_bindir}/nli
%attr(0755,root,root) %{_sbindir}/nli-controller-setup
%attr(0755,root,root) %{_sbindir}/nli-supabase-setup
%attr(0755,root,root) %{_sbindir}/nli-admin-reset
%dir %attr(0750,root,nli) %{_sysconfdir}/nli
%config(noreplace) %attr(0640,root,nli) %{_sysconfdir}/nli/controller.json
%config(noreplace) %attr(0640,root,nli) %{_sysconfdir}/nli/controller.env
%config(noreplace) %attr(0640,root,nli) %{_sysconfdir}/nli/geoip.env
%ghost %attr(0600,root,root) %{_sysconfdir}/nli/controller-bootstrap.env
%attr(0644,root,root) /usr/lib/systemd/system/nli-controller-agent.service
%attr(0644,root,root) /usr/lib/systemd/system/nli-controller-bootstrap.service
%attr(0644,root,root) /usr/lib/systemd/system/nli-geoip-update.service
%attr(0644,root,root) /usr/lib/systemd/system/nli-geoip-update.timer
%dir %attr(0755,root,root) %{_datadir}/nli
%dir %attr(0755,root,root) %{_datadir}/nli/webview_ver13
%attr(0644,root,root) %{_datadir}/nli/webview_ver13/*
%dir %attr(0755,root,root) %{_datadir}/nli/supabase
%attr(0644,root,root) %{_datadir}/nli/supabase/nli_schema.sql
%dir %attr(0750,nli,nli) /var/lib/nli-controller
%doc %{_docdir}/%{name}/README.md

%changelog
* Fri Oct 16 2026 Nexora Engineering <engineering@nexora.local> - 13.0.0-1
- Add AWS RHEL/Rocky wrappers, private VPC defaults, Cloudflare Tunnel, and hosted-WebApp deployment contracts

* Tue Oct 13 2026 Nexora Engineering <engineering@nexora.local> - 12.0.0-1
- Add enterprise bootstrap, protected password file, and corporate PKI setup

* Sun Oct 11 2026 Nexora Engineering <engineering@nexora.local> - 12.0.0-5
- Add public HTTPS setup modes, quarantine operations, and API monitoring

* Sat Oct 10 2026 Nexora Engineering <engineering@nexora.local> - 12.0.0-4
- Require an exact release identity and complete bundle checksum verification

* Sat Oct 10 2026 Nexora Engineering <engineering@nexora.local> - 12.0.0-3
- Validate distro-suffixed V12 RPM release metadata in guided installers

* Wed Oct 07 2026 Nexora Engineering <engineering@nexora.local> - 12.0.0-1
- Build Nexora Log Inetelligence Ver12 from source with Supabase and Vercel delivery

* Tue Oct 06 2026 Nexora Engineering <engineering@nexora.local> - 5.0.0-4
- Replace the legacy cloud adapter with RLS-protected Supabase metadata RPCs
- Run Assurance through production ParserPacks and preserve the full static prefix
- Retain encrypted fixtures until manual purge and support authenticated .log download
- Keep Fleet, Parser Catalogue, and Migration Center actionable on empty collections

* Tue Oct 06 2026 Nexora Engineering <engineering@nexora.local> - 5.0.0-3
- Fail closed until the WebView verifies the controller identity and login contract
- Add explicit first-run controller setup with private-network TLS bootstrap
- Consume enrollment tokens once, support deliberate rotation, and declare hostname dependency
- Preserve cookie, CSRF, and bearer authentication through the fixed-origin Vercel proxy

* Tue Oct 06 2026 Nexora Engineering <engineering@nexora.local> - 5.0.0-2
- Repair relative WebView assets and ship production-only frontend files
- Add premium role-aware console and interactive cloud metadata setup

* Mon Oct 05 2026 Nexora Engineering <engineering@nexora.local> - 5.0.0-1
- Embed byte-verified webview_ver5 assets and hardened controller service
- Preserve controller configuration, secrets, and durable state
