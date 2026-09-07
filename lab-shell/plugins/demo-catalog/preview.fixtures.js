/*
  Sample props for the shared preview harness (shared/mfe-preview/), keyed by
  contribution id. Optional and data-only — the harness invents placeholders
  when this file is absent, which is why the other plugins do not have one.

  `intro` needs a real demo id: the view looks the demo up by id and there is
  nothing sensible to draw for one that does not exist.
*/
export default {
  intro: { id: '01-dictionary' },
}
