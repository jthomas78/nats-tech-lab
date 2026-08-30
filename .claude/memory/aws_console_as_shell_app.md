---
name: aws_console_as_shell_app
description: AWS Console as a mental model for our app shell — AWS's documented MFE discovery/shell pattern, and where our contribution-point model deliberately goes further
metadata:
  type: reference
---

Source: `aws-console-to-openmrs-stack.md` (chat transcript at repo root), AWS-Console
section only. Reference material for the app-shell work
(`ARCHITECTURE-APP-SHELL.md`, `.claude/plans/Application-Shell-Microfrontend-Plan.md`).

**AWS does not publish the Management Console's actual implementation** — so it is a
mental model, not a citable mechanism. What AWS *does* document (Prescriptive
Guidance, "Micro-frontends on AWS") is the same shape we're building:

- A **shell application** that calls a **micro-frontend discovery service**, gets back
  manifests (name, URL, version, fallback behaviour), and loads bundles into defined
  areas of the page. The shell owns *discovery, loading, and rendering at runtime*.
  - https://docs.aws.amazon.com/prescriptive-guidance/latest/micro-frontends-aws/composition-approaches.html
- A **full-stack variation** where each micro-frontend owns its own backend — i.e. a
  service ships a backend capability *and* a frontend capability, instead of N backend
  teams plus one giant frontend team.
  - https://docs.aws.amazon.com/prescriptive-guidance/latest/micro-frontends-aws/introduction.html
- Frontend bounded contexts own their UI, state, and business flow; **cross-cutting
  concerns such as the design system are deliberately shared** — which is exactly why
  `shared/unifi-theme` + `shared/ui-shell` are shared here rather than per-plugin.

The Console's *user-visible* shape backs this up: persistent global concerns (top bar,
account/region context, navigation, notifications, auth/session) with service-specific
experiences underneath. EC2 → S3 → IAM feels like one app despite being separately
owned product domains. That coherent-platform feel is the target.

**Where our design intentionally goes beyond AWS:** AWS's guidance composes from
relatively *coarse* micro-frontends. Ours is finer-grained via explicit **contribution
points** — a plugin contributes a route, a left-menu entry, a dashboard widget, a
detail-page tab, a toolbar action — so the shell is closer to
**AWS Console + VS Code extension model + micro-frontends**, with per-kind registries
(navigation / route / panel) behind one extension API. See
[[linebooker_platform_vs_tenant_service_split]] for which services would own plugins.

The same transcript covers plugin-disappearance lifecycle (scoped, disposable
registrations; deployment-availability vs operational-health separation) and the
OpenMRS O3 / Backstage / Grafana comparisons — those are separate topics, not captured
here.
