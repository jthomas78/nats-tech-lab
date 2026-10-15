# Demo 07 — set up WorkOS

Do this once, before exercise 01. It is a prerequisite for every exercise
that uses a real WorkOS identity. Back to the [demo README](README.md).

WorkOS is a hosted service. It does not run on your machine. You set it up
once in the WorkOS dashboard, then you write one local file. Labels in the
dashboard can change; the list says *what* to set, not where to click.

**In the WorkOS dashboard**

1. Choose one WorkOS **project** (or make one for this demo). Then open
   that project's **Staging** environment. Leave Production alone. Do
   every step below, the client ID too, in this same project and
   environment.
2. Set up **AuthKit** (the hosted log-in page). Turn on one sign-in method
   that your test users can use, for example email + password.
3. Turn on **CLI Auth** (the device log-in from a terminal). It is a public
   client: it needs the client ID only, no API key and no secret
   ([WorkOS CLI Auth](https://workos.com/docs/authkit/cli-auth)).
4. Add two **environment roles** (in the environment's Roles section, not
   inside an organization). Give them exactly these **slugs**: `dispatcher`
   and `viewer`. A slug is the role's fixed ID, and it is the value the
   token carries. Do not make them as organization roles: WorkOS adds an
   `org` prefix to those slugs, so they would not match. Keep the default
   role (`member`) as it is
   ([WorkOS roles](https://workos.com/docs/authkit/roles-and-permissions)).
5. Make one **organization**, for example `d07-lab`. Write down its
   organization ID (it starts `org_`); step 8 needs it. The callout
   service lets in users of this organization only. A role only
   appears in a token when the user logs in inside an organization.
6. Add three **test users** to that one organization. Set each role on the
   user's **organization membership** (the user's link to `d07-lab`):

   | Test user | Role in the organization | Used for |
   |---|---|---|
   | dispatcher | `dispatcher` | admission, publish and read (exercises 01, 02) |
   | viewer | `viewer` | read only (exercise 02) |
   | no-role | the default role only | the "unknown role" refusal (exercise 03) |

   Put each user in this organization only. That is a constraint of this
   demo: `lab/workos-login.sh` has no step to choose an organization, so
   it treats any organization choice as a failed log-in.
7. Copy the Staging environment's **client ID** (it starts `client_`).

**On your machine**

Run steps 8 and 9 from the repository root (the folder that holds
`demos/`). The paths below start there.

8. Write the client ID and the organization ID into the env file. Replace
   `client_XXXX` with your Staging client ID from step 7, and `org_XXXX`
   with your `d07-lab` organization ID from step 5. The file is in
   `.gitignore`; never commit it.

   ```bash
   printf 'D07_WORKOS_CLIENT_ID=client_XXXX\nD07_WORKOS_ORG_ID=org_XXXX\n' > demos/07-auth-callout-workos/.env.workos
   ```

9. Do a preliminary check of the set-up:

   ```bash
   demos/07-auth-callout-workos/lab/workos-login.sh dispatcher
   ```

   The word `dispatcher` is only the name of the saved file. It does not
   pick the user. You pick the user in the browser: log in **as the
   dispatcher test user**.

   The script prints a URL and a code. Open the URL, log in, and confirm the
   code. If you do not finish before the code expires (WorkOS says how
   long), the script stops; run it again. The script saves the token in
   `.run/workos/` and prints its claims, never the token.

   The set-up looks right when both are true:

   - `org_id` is **your `d07-lab` organization ID** from step 5. The
     script compares it with `D07_WORKOS_ORG_ID` and prints
     `org_id   matches D07_WORKOS_ORG_ID`. Any other organization ID is
     wrong, and the callout service refuses it.
   - `"role": "dispatcher"`.

   If `org_id` or `role` is missing or different, look at steps 4–6 again:
   the role slug, the organization, and the user's membership. A missing
   claim tells you where to look. It does not prove which step is wrong.

   This check only **decodes** the token. It does not check the signature,
   so it does not prove that the token is genuine. The Go callout service
   checks the signature against the WorkOS keys, in exercise 01.
