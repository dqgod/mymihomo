# zashboard in mymihomo

Upstream: https://github.com/Zephyruso/zashboard
Imported release: **v3.30.0**
Source archive: https://github.com/Zephyruso/zashboard/archive/refs/tags/v3.30.0.tar.gz
License: MIT; retain `LICENSE` and upstream copyright.

This directory contains upstream source, not a downloaded build. The parent
repository's `dev` branch owns the local changes. `package.json` keeps the upstream
version; `pnpm-lock.yaml` pins dependencies, including the local QR code generator.

Local integration:

- `src/views/SubscriptionsPage.vue`: remote/local profiles, update schedules,
  usage metadata, YAML and JavaScript extensions, file/section editing, sharing.
- Router, route constants and language labels: native subscription navigation.
- `src/store/setup.ts`, `src/App.vue`, `src/api/http.ts`: automatically connect to
  the same-origin authenticated gateway; never put core secrets in browser storage.
- Sidebar: logout. `src/assembly/version.ts`: disable automatic distribution overwrite.
- `vite.config.ts`: disable service-worker caching for authenticated pages.
- Management API, authentication, scheduling and scripts live in `cmd/mymihomo`.

For an upstream upgrade, compare a new release against this recorded tag, merge
upstream changes into this directory, preserve the integration above, and update
this record. Run `pnpm type-check`, `pnpm build:no-fonts`, Go tests and gateway/browser
integration checks before rebuilding the deployment. Do not use Mihomo's dashboard
upgrade endpoint: it would replace this fork with upstream static assets.

Type-check compatibility: browser timer assignments in `BackendSwitchToast.vue`,
`use-backend-reachability.ts` and `helper/notification.ts` use `window.setTimeout`
to avoid Node timer overloads introduced by transitive type declarations.

Additional maintained integration: remote source read-only views and runtime
configuration viewer; `components/common/SearchableEditor.vue` for scoped find;
`components/proxies/ProxyGroupActions.vue`, proxy headers, list rendering and scroll
helpers for per-group controls; `BackendPortsGrid.vue` displays Docker ports read-only.

Access controls are in `components/settings/AccessSettings.vue` on the settings
page (including the mobile category index). Logout buttons were removed from the
subscription page and main sidebar. Password rotation is handled by the Go gateway.

Runtime profile editing is implemented in `components/runtime/RuntimeEditor.vue`
and `store/runtime-edits.ts`, opened by the proxy/rule page toolbars. Rule-card and
table controls lead to the persistent editor instead of core-only disable toggles.
The source retains upstream monitoring views alongside these managed edit views.

`SearchablePolicySelect.vue` provides the runtime rule policy list. It opens all
available policies independently of the current value, supports explicit search
and keyboard navigation, and renders above editor dialogs on mobile and desktop.
