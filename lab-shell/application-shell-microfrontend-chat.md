# Chat Transcript

## User

I’m looking for ways to create a harness/shell app that is empty to start off with but which allows for a microservice to inject frontend navbar and menu items and info panels and into defined areas in the UI. This would allow any service to inject a frontend feature set.

## Assistant

Yes — what you’re describing is essentially a **frontend application shell with a plugin/contribution model**, usually implemented using **micro-frontends** plus a runtime registry. The important distinction is that services shouldn’t directly manipulate the shell DOM; they should register *contributions* such as routes, menu items, panels, actions, and widgets through a defined contract.

Yes. This is a well-established architecture, and it maps particularly well to the kind of service-oriented logistics platform you’ve been designing.

I would describe it as an **Extensible Application Shell + Micro-Frontend Plugin Architecture**.

The key idea is:

```text
Backend microservice
      │
      ├── API / NATS / events
      │
      └── Frontend Plugin
             │
             ├── routes
             ├── nav items
             ├── menu items
             ├── dashboard panels
             ├── detail panels
             ├── actions
             └── settings
                      │
                      ▼
              APPLICATION SHELL
```

The shell starts almost empty. It owns only the things that must be globally consistent: authentication, layout, routing infrastructure, navigation regions, theming, permissions, notification infrastructure, and the plugin runtime.

## 1. Don't have services "inject HTML"

I would avoid allowing arbitrary services to inject markup directly.

Instead, define **extension points / slots** in the shell:

```text
┌────────────────────────────────────────────────────────────┐
│ Application Shell                                          │
├────────────────────────────────────────────────────────────┤
│ [ logo ]  [ Navbar contributions ...................... ]   │
├───────────────┬────────────────────────────────────────────┤
│               │                                            │
│ Main Menu     │              Router Outlet                 │
│               │                                            │
│ [slot]        │     dynamically loaded feature             │
│               │                                            │
│               │                                            │
│               ├───────────────────────────────┬────────────┤
│               │                               │            │
│               │                               │ Info Panel │
│               │                               │   [slot]   │
└───────────────┴───────────────────────────────┴────────────┘
```

A plugin then says:

```ts
export const plugin = {
  id: "fleet",

  contributions: {
    navigation: [
      {
        id: "fleet",
        label: "Fleet",
        icon: "truck",
        route: "/fleet"
      }
    ],

    routes: [
      {
        path: "/fleet",
        component: FleetPage
      }
    ],

    panels: [
      {
        slot: "organisation.details.sidebar",
        component: FleetSummaryPanel
      }
    ]
  }
};
```

The shell decides **where and whether** those contributions are rendered.

That gives you much stronger architectural control.

---

# 2. Think of it as a plugin contract

For your architecture I'd define something like:

```ts
interface FrontendPlugin {
  id: string;
  version: string;

  activate(context: ShellContext): void | Promise<void>;
}
```

And the context exposes controlled APIs:

```ts
interface ShellContext {
  navigation: NavigationRegistry;
  routes: RouteRegistry;
  panels: PanelRegistry;
  actions: ActionRegistry;
  widgets: WidgetRegistry;

  auth: AuthService;
  permissions: PermissionService;

  events: EventBus;
}
```

Then your Fleet plugin could do:

```ts
export async function activate(shell: ShellContext) {

  shell.navigation.register({
    id: "fleet",
    label: "Fleet",
    icon: "truck",
    route: "/fleet"
  });

  shell.routes.register({
    path: "/fleet",
    component: () => import("./FleetPage.vue")
  });

  shell.panels.register(
    "organisation.details.sidebar",
    {
      id: "fleet-summary",
      component: () => import("./FleetSummary.vue")
    }
  );
}
```

This is considerably cleaner than having the shell understand what "Fleet" is.

The shell only understands:

```text
NavigationContribution
RouteContribution
PanelContribution
ActionContribution
WidgetContribution
```

---

# 3. Your microservices can effectively become vertical slices

This is where the architecture becomes particularly interesting.

Imagine:

```text
Fleet Service
├── Backend
│   ├── REST/gRPC
│   ├── NATS subjects
│   ├── Temporal activities
│   └── PostgreSQL
│
└── Frontend
    ├── FleetPage.vue
    ├── VehiclePage.vue
    ├── FleetSummaryPanel.vue
    └── plugin.ts
```

Similarly:

```text
Documents Service
├── Backend
└── Frontend Plugin
    ├── DocumentsPage
    ├── DocumentUploader
    └── DocumentPanel
```

and:

```text
Transporter Service
├── Backend
└── Frontend Plugin
    ├── TransporterList
    ├── TransporterDetails
    ├── CompliancePanel
    └── TransporterNav
```

The resulting UI isn't really one giant frontend anymore.

It is:

```text
                         APP SHELL
                            │
          ┌─────────────────┼─────────────────┐
          │                 │                 │
       Fleet UI        Documents UI      Orders UI
          │                 │                 │
      Fleet svc        Documents svc     Orders svc
```

This is essentially the **micro-frontend equivalent of vertical-slice architecture**.

---

# 4. Runtime discovery makes it much more powerful

You don't necessarily want this:

```ts
import FleetPlugin from "@myapp/fleet";
import OrdersPlugin from "@myapp/orders";
import DocsPlugin from "@myapp/documents";
```

because now the shell has to be rebuilt whenever a service is added.

Instead:

```text
Shell
  │
  ▼
GET /platform/frontend-plugins
  │
  ▼
[
  {
    "id": "fleet",
    "url": "https://fleet.example.com/ui/remoteEntry.js"
  },
  {
    "id": "orders",
    "url": "https://orders.example.com/ui/remoteEntry.js"
  },
  {
    "id": "documents",
    "url": "https://documents.example.com/ui/remoteEntry.js"
  }
]
```

The shell dynamically loads them.

Webpack's Module Federation explicitly supports remote modules being loaded at runtime, including dynamic remote containers.

So deployment can become:

```text
Deploy Fleet Service v2.3
        │
        ├── fleet-service container
        └── fleet-ui remoteEntry.js
                  │
                  ▼
            Registry updated
                  │
                  ▼
         Shell discovers plugin

       NO SHELL REBUILD
```

That's probably the characteristic you're ultimately looking for.

---

# 5. Module Federation is a very good fit

For a Vue 3 application, I'd seriously consider:

**Vite + Vue 3 + Module Federation + your own contribution registry.**

Module Federation solves:

> How does independently built JS code get loaded into my application?

Your plugin API solves:

> What is that loaded code allowed to contribute?

Those are separate concerns.

```text
        Module Federation
              │
              │ loads
              ▼
       Frontend Plugin
              │
              │ registers
              ▼
     Contribution Registry
              │
       ┌──────┼────────┐
       ▼      ▼        ▼
      Nav   Routes   Panels
```

That separation is important.

Module Federation itself isn't your application architecture. It's the **runtime loading mechanism**.

The Webpack documentation describes exactly this shell/remote model: separate builds can form one application, with remotely deployed modules dynamically loaded into the host.

---

# 6. Extension points are the part I'd emphasize most

For example, define named slots:

```text
platform.nav.primary

platform.nav.user

dashboard.main

dashboard.sidebar

organisation.details.tabs

organisation.details.sidebar

transporter.details.tabs

transporter.details.actions

shipment.details.tabs

shipment.details.sidebar

admin.settings
```

A plugin can contribute:

```ts
shell.panels.register(
  "transporter.details.tabs",
  {
    id: "compliance",
    title: "Compliance",
    order: 30,
    component: ComplianceTab
  }
);
```

Another service:

```ts
shell.panels.register(
  "transporter.details.tabs",
  {
    id: "fleet",
    title: "Fleet",
    order: 40,
    component: FleetTab
  }
);
```

Now your transporter page becomes:

```text
Transporter: ABC Logistics

[ Overview ]
[ Compliance ]  ← compliance service
[ Fleet ]       ← fleet service
[ Documents ]   ← document service
[ Insurance ]   ← insurance service
[ Activity ]    ← audit service
```

And **the Transporter page doesn't need to know those modules exist**.

That's an extremely useful property.

---

# 7. Even the navbar can be registry-driven

Instead of:

```vue
<NavItem to="/orders">Orders</NavItem>
<NavItem to="/fleet">Fleet</NavItem>
```

the shell does:

```vue
<NavItem
  v-for="item in navigationRegistry.items"
  :key="item.id"
  :to="item.route"
/>
```

A service registers:

```ts
navigation.register({
  id: "fleet",
  label: "Fleet",
  icon: "truck",
  route: "/fleet",

  permission: "fleet.read",

  placement: "primary",

  order: 40
});
```

So RBAC becomes natural too:

```text
Plugin contribution
       │
       ▼
permission = fleet.read
       │
       ▼
Shell permission evaluator
       │
       ├── allowed → render
       │
       └── denied → omit
```

The plugin doesn't need to implement global authorization UI behaviour itself.

---

# 8. You can go considerably beyond menus

I would define contribution types roughly like this:

| Contribution | Example |
|---|---|
| `navigation` | Fleet menu |
| `route` | `/fleet/vehicles/:id` |
| `panel` | Insurance status sidebar |
| `tab` | Documents tab |
| `action` | "Suspend transporter" |
| `widget` | Active fleet dashboard card |
| `toolbar` | Shipment actions |
| `settings` | Fleet configuration page |
| `command` | "Create vehicle" |
| `searchProvider` | Vehicles in global search |
| `notificationRenderer` | Fleet-specific notification |
| `entityRenderer` | Vehicle summary/card |

That gives you a real **application platform**, rather than just a collection of microfrontends.

---

# 9. There's an existing architecture very close to what you're describing

One useful current example is OpenMRS O3.

Its frontend architecture uses an application shell plus dynamically loaded frontend modules. Modules can register pages, extensions, modals, workspaces, and feature flags; the shell discovers the modules and dynamically loads their Module Federation containers.

Conceptually:

```text
                    OpenMRS Shell
                         │
                  Extension Slots
                         │
          ┌──────────────┼──────────────┐
          ▼              ▼              ▼
       module A       module B       module C
```

It's worth studying because this is closer to what you're describing than a typical "microfrontend per route" tutorial.

Another conceptually similar approach describes plugins explicitly registering **contributions** into host registries—routes, nav items, settings panels, toolbar slots and components.

---

# 10. single-spa is another option, but slightly different

single-spa is designed specifically for composing independently developed frontend applications and can lazy-load those applications at runtime.

It is especially attractive if you want:

```text
React application
       +
Vue application
       +
Angular application
       ↓
same shell
```

There are even recent examples combining single-spa with Module Federation where the shell dynamically registers header, footer, and content frontend modules.

But because you're already using Vue, I wouldn't necessarily introduce single-spa initially.

I'd start with:

```text
Vue 3
  +
Vite
  +
Module Federation
  +
your contribution API
```

and only introduce a heavier orchestration layer if you actually need multiple frontend frameworks.

---

# 11. I would separate the service registry from the frontend registry

In your platform I'd probably have:

```text
                    PLATFORM
                       │
             ┌─────────┴──────────┐
             │                    │
      Service Registry      UI Plugin Registry
             │                    │
             ▼                    ▼
         fleet.v1              fleet-ui
       orders.v1              orders-ui
     documents.v1            documents-ui
```

Potentially one manifest:

```json
{
  "service": "fleet",

  "api": {
    "subject": "za.fleet.>"
  },

  "frontend": {
    "module": "fleet-ui",
    "entry": "/ui/remoteEntry.js",
    "version": "2.4.1"
  }
}
```

This doesn't mean the browser should discover arbitrary NATS services directly. The **platform should curate which frontend modules are trusted and enabled**.

That's an important security boundary.

---

# 12. The resulting architecture fits your existing system nicely

With your organisation/service model I could see it becoming:

```text
LOGISTICS PLATFORM
│
├── UI Shell
│   ├── Authentication
│   ├── Organisation context
│   ├── Operator context
│   ├── Theme
│   ├── Router
│   ├── Navigation
│   ├── Contribution Registry
│   └── Plugin Runtime
│
├── UI Plugins
│   │
│   ├── Marketplace
│   ├── Orders
│   ├── Shipping
│   ├── Fleet
│   ├── Documents
│   ├── Compliance
│   ├── Insurance
│   ├── Tracking
│   └── Billing
│
├── Domain Services
│   ├── Marketplace Service
│   ├── Orders Service
│   ├── Shipping Service
│   ├── Fleet Service
│   ├── Document Service
│   └── ...
│
└── Platform
    ├── NATS
    ├── Temporal
    ├── Identity
    └── Observability
```

And importantly:

```text
Installing/enabling a service
             │
             ▼
      Service advertises
       UI contribution
             │
             ▼
       Shell discovers
             │
       ┌─────┴─────┐
       ▼           ▼
    API exists   UI appears
```

That gives you something approaching a **modular logistics operating system**: the shell itself knows very little about logistics features, while services extend it through a stable UI SDK.

I'd make **`Shell SDK + Contribution Registry + Runtime Plugin Manifest + Module Federation`** the four fundamental pieces. Of those, the **Contribution Registry contract** is actually the most important architectural decision; Module Federation can be swapped out later without changing your service-facing extension model.

## User

save the entire chat to a markdown file.
